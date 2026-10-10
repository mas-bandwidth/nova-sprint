#!/usr/bin/env python3
"""Sprint dashboard server: serves one page on 127.0.0.1 and a cached JSON
snapshot of `nova-sprint where --json`.

One poller thread runs the sprint command back to back (the next read starts
when the last returns, floored at DASHBOARD_MIN_INTERVAL), so reads never
overlap however many pages are open; read times are logged once a minute. Pages read /api/sprint, which only returns the cache. A failed poll keeps
the last good snapshot and records the failure; the page shows it as stale.
Nothing from the command's stderr is kept or served (the wrapper's secrets
line and any error text stay out of the page).
"""
import html
import collections
import json
import os
import re
import subprocess
import threading
import time
import zlib
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
# ROOT holds the served files (the page, the script, the logos). run.sh serves live/, a copy
# promoted by deploy.sh only after check.mjs passes; the check server serves the source dir.
ROOT = os.path.abspath(os.environ.get("DASHBOARD_ROOT", HERE))
# The check server reads the live server's snapshot instead of the sprint, so a check never
# adds load to the sprint server.
UPSTREAM = os.environ.get("DASHBOARD_UPSTREAM", "")
HOST = os.environ.get("DASHBOARD_HOST", "127.0.0.1")
PORT = int(os.environ.get("DASHBOARD_PORT", "7390"))
# Reads run back to back: the next starts as soon as the last returns, but no
# sooner than MIN_INTERVAL seconds after the last one started (0 = no floor).
MIN_INTERVAL = float(os.environ.get("DASHBOARD_MIN_INTERVAL", "0"))
LOG_EVERY = 60.0  # seconds between read-time summary lines in the log
POLL_TIMEOUT = float(os.environ.get("DASHBOARD_POLL_TIMEOUT", "60"))
SPRINT_CMD = os.environ.get("SPRINT_CMD", "")  # path to the coordinator seat wrapper
SPRINT_SERVER = os.environ.get("NOVA_SPRINT_SERVER", "127.0.0.1:6390")

STATIC = {
    "/app.js": ("app.js", "text/javascript; charset=utf-8"),
    "/nunito-800.woff2": ("nunito-800.woff2", "font/woff2"),
    "/OFL.txt": ("OFL.txt", "text/plain; charset=utf-8"),
}

# The logo slot: when logo.svg exists beside index.html, its drawing is put in
# the title (32 px, currentColor) and served as the favicon; when it does not,
# the slot and the favicon link render nothing. Read on every request, so a
# new logo.svg shows on the next page load without a restart.
LOGO = os.path.join(ROOT, "logo.svg")
FAVICON_STYLE = ("<style>svg{color:#121417}@media (prefers-color-scheme:dark)"
                 "{svg{color:#eef0f3}}</style>")


def read_logo():
    """(viewBox, inner markup) of logo.svg, or None when there is none."""
    try:
        with open(LOGO, encoding="utf-8") as f:
            text = f.read()
    except OSError:
        return None
    m = re.search(r"<svg\b([^>]*)>(.*)</svg>", text, re.S | re.I)
    if not m:
        return None
    vb = re.search(r'viewBox\s*=\s*"([^"]+)"', m.group(1))
    return (vb.group(1) if vb else "0 0 32 32"), m.group(2)


def build_id():
    """Changes whenever a served file changes: the page reloads itself on it,
    and it versions the script link so no cache serves an old app.js."""
    names = ("index.html", "app.js", "logo.svg") + RASTERS
    sig = []
    for n in names:
        try:
            st = os.stat(os.path.join(ROOT, n))
            sig.append("%s:%d:%d" % (n, st.st_mtime_ns, st.st_size))
        except OSError:
            pass
    # a signature, not the newest time: a file put back with an older time still changes it
    return "%08x" % zlib.crc32("|".join(sig).encode())


# Interim raster logo (logo.webp, else logo.png), used only when there is no
# logo.svg. Its white background is keyed out with ffmpeg into logo-icon.png
# (the title, 144 px tall for a 48 px box) and favicon.png (64 px square);
# both are rebuilt whenever the raster is newer. LOGO_CROP (w:h:x:y, ffmpeg
# crop) trims the photo's white margins; the default fits the shoe image,
# and an empty value means no crop.
# Tiles are icons already (rounded square, transparent outside): shown as is
# from sips downscales. Keyed rasters are photos on white.
TILES = ("logo-robot.webp", "logo-stella.png")
RASTERS = TILES + ("logo.webp", "logo.png")
KEYED = ("logo.webp", "logo.png")  # photos on white: background keyed out
LOGO_CROP = os.environ.get("LOGO_CROP", "1420:660:90:120")
FFMPEG = os.environ.get("FFMPEG", "/opt/homebrew/bin/ffmpeg")
_derive_lock = threading.Lock()


def image_type(body):
    """The image type from the bytes (Stella's .png is WebP inside)."""
    if body[:4] == b"RIFF" and body[8:12] == b"WEBP":
        return "image/webp"
    if body[:8] == b"\x89PNG\r\n\x1a\n":
        return "image/png"
    if body[:3] == b"\xff\xd8\xff":
        return "image/jpeg"
    return "application/octet-stream"


def raster_logo():
    if os.path.exists(LOGO):
        return None
    for n in RASTERS:
        if os.path.exists(os.path.join(ROOT, n)):
            return n
    return None


def derived(kind):
    """Path of logo-icon.png or favicon.png for the raster logo, rebuilt when stale."""
    src = raster_logo()
    if not src:
        return None
    keyed = src in KEYED
    src = os.path.join(ROOT, src)
    if not keyed:
        return None  # a tile is already an icon: see tile_copy
    out = os.path.join(ROOT, "logo-icon.png" if kind == "icon" else "favicon.png")
    if keyed:
        crop = ("crop=%s," % LOGO_CROP) if LOGO_CROP else ""
        key = "colorkey=0xFFFFFF:0.18:0.12,"
    else:
        crop = key = ""
    vf = crop + key + ("scale=-1:144:flags=lanczos,format=rgba" if kind == "icon" else
                       "scale=64:64:force_original_aspect_ratio=decrease:flags=lanczos,"
                       "pad=64:64:(ow-iw)/2:(oh-ih)/2:color=0x00000000,format=rgba")
    with _derive_lock:
        if not os.path.exists(out) or os.stat(out).st_mtime_ns != os.stat(src).st_mtime_ns:
            try:
                subprocess.run([FFMPEG, "-loglevel", "error", "-y", "-i", src, "-vf", vf, out],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30, check=True)
                st = os.stat(src); os.utime(out, ns=(st.st_atime_ns, st.st_mtime_ns))
            except (OSError, subprocess.SubprocessError):
                return None
    return out


TILE_SIZES = (192, 384)


def tile_copy(size):
    """<stem>-<size>.png, a sips downscale of the winning tile (logo-robot.webp,
    else logo-stella.png), rebuilt when the source is newer."""
    src = raster_logo()
    if src not in TILES or size not in TILE_SIZES:
        return None
    out = os.path.join(ROOT, "%s-%d.png" % (os.path.splitext(src)[0], size))
    src = os.path.join(ROOT, src)
    with _derive_lock:
        if not os.path.exists(out) or os.stat(out).st_mtime_ns != os.stat(src).st_mtime_ns:
            try:
                subprocess.run(["/usr/bin/sips", "-s", "format", "png", "-Z", str(size), src, "--out", out],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30, check=True)
                st = os.stat(src); os.utime(out, ns=(st.st_atime_ns, st.st_mtime_ns))
            except (OSError, subprocess.SubprocessError):
                return None
    return out


def index_page():
    with open(os.path.join(ROOT, "index.html"), encoding="utf-8") as f:
        page = f.read()
    page = page.replace('src="app.js"', 'src="app.js?v=%s"' % build_id())
    logo = read_logo()
    slot = icon = ""
    if logo:
        slot = ('<svg id="logo" viewBox="%s" width="32" height="32" fill="currentColor" '
                'aria-hidden="true">%s</svg>' % (html.escape(logo[0]), logo[1]))
        icon = '<link rel="icon" type="image/svg+xml" href="/favicon.svg">'
    elif tile_copy(192) and tile_copy(384):
        v = build_id()
        slot = ('<img id="logo" class="logo-tile" src="/logo-tile-192.png?v=%s" '
                'srcset="/logo-tile-192.png?v=%s 1x, /logo-tile-384.png?v=%s 2x" alt="">' % (v, v, v))
        icon = ('<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=%s">'
                '<link rel="apple-touch-icon" href="/logo-tile-192.png?v=%s">'
                '<link rel="preload" as="image" href="/logo-tile-192.png?v=%s" '
                'imagesrcset="/logo-tile-192.png?v=%s 1x, /logo-tile-384.png?v=%s 2x">' % (v, v, v, v, v))
    elif derived("icon"):
        v = build_id()
        slot = '<img id="logo" class="raster" src="/logo-icon.png?v=%s" alt="" height="48">' % v
        if derived("favicon"):
            icon = '<link rel="icon" type="image/png" href="/favicon.png?v=%s">' % v
    return page.replace("<!--LOGO-->", slot).replace("<!--FAVICON-->", icon).encode()


def favicon():
    logo = read_logo()
    if not logo:
        return None
    return ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="%s" fill="currentColor">%s%s</svg>'
            % (html.escape(logo[0]), FAVICON_STYLE, logo[1])).encode()

_lock = threading.Lock()
_state = {
    "ok": False,          # did the last attempt succeed
    "data": None,         # last good snapshot
    "fetchedAt": None,    # the last good snapshot's own time (its `at`), not when it was read
    "dataAgeSeconds": None,  # how old that snapshot was when its read returned
    "attemptAt": None,    # when the last attempt finished
    "error": None,        # short reason of the last failure, never command output
    "readSeconds": None,  # wall time of the last read, success or not
    "minInterval": MIN_INTERVAL,
    "throughput": None,         # cards landed per hour over the last hour
    "throughputMinutes": 0,     # how many minutes of samples that rate covers
}

# Throughput: a ring of (monotonic time, landed) samples, one per good read,
# kept for an hour. The rate is shown once ten minutes of samples exist. A
# drop in landed (a cleared sprint) starts the ring again. It lives in memory,
# so a restart starts a new ten minutes.
WINDOW, MIN_SPAN = 3600.0, 600.0
_samples = collections.deque()


def sample_landed(landed, now):
    if _samples and landed < _samples[-1][1]:
        _samples.clear()
    _samples.append((now, landed))
    while _samples and now - _samples[0][0] > WINDOW:
        _samples.popleft()
    span = now - _samples[0][0]
    if span < MIN_SPAN:
        return None, span / 60
    return (landed - _samples[0][1]) * 3600.0 / span, span / 60


def now_iso():
    return datetime.now(timezone.utc).isoformat()


def data_time(data):
    """The snapshot's own time (where's `at`), as a UTC datetime, or None when it has none
    or it does not parse. A copy pulled from an upstream is as old as its `at`, never as
    fresh as the pull (2026-10-10: the mirror stamped its own fetch time over a 7 s old
    snapshot and looked fresh)."""
    at = data.get("at") if isinstance(data, dict) else None
    if not isinstance(at, str):
        return None
    # Go writes up to 9 fraction digits and a Z; fromisoformat takes 6 and an offset
    s = re.sub(r"(\.\d{6})\d+", r"\1", at.strip()).replace("Z", "+00:00")
    try:
        t = datetime.fromisoformat(s)
    except ValueError:
        return None
    return t.astimezone(timezone.utc) if t.tzinfo else None


def poll_once():
    if UPSTREAM:
        import urllib.request
        try:
            with urllib.request.urlopen(UPSTREAM, timeout=10) as r:
                j = json.loads(r.read())
        except (OSError, ValueError):
            return None, "upstream unreachable"
        return (j.get("data"), None) if j.get("data") else (None, "upstream has no data")
    if not SPRINT_CMD:
        return None, "SPRINT_CMD is not set"
    env = dict(os.environ, NOVA_SPRINT_SERVER=SPRINT_SERVER)
    try:
        p = subprocess.run([SPRINT_CMD, "where", "--json"], env=env,
                           stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                           timeout=POLL_TIMEOUT, text=True)
    except subprocess.TimeoutExpired:
        return None, "where timed out after %ds" % POLL_TIMEOUT
    except OSError as e:
        return None, "could not run the sprint command (%s)" % e.__class__.__name__
    lines = [l for l in p.stdout.splitlines() if not l.startswith("SECRETS")]
    if p.returncode != 0:
        return None, "where exited %d" % p.returncode
    try:
        data = json.loads("\n".join(lines))
    except ValueError:
        return None, "where printed no JSON"
    if not isinstance(data, dict) or "tables" not in data:
        return None, "where JSON has no tables"
    return data, None


def poller():
    times, fails, since = [], 0, time.monotonic()
    while True:
        start = time.monotonic()
        data, err = poll_once()
        took = time.monotonic() - start
        times.append(took)
        with _lock:
            _state["attemptAt"] = now_iso()
            _state["readSeconds"] = round(took, 3)
            if data is not None:
                try:
                    rate, minutes = sample_landed(int(data.get("landed", 0)), time.monotonic())
                except (TypeError, ValueError):
                    rate, minutes = None, 0
                # fetchedAt is the snapshot's own time (where's `at`), so a stale upstream reads
                # stale here; the read's own time is attemptAt. dataAgeSeconds: how old it was
                # when this read returned.
                t = data_time(data)
                fetched = t.isoformat() if t else _state["attemptAt"]
                age = None if t is None else round((datetime.now(timezone.utc) - t).total_seconds(), 3)
                _state.update(ok=True, data=data, fetchedAt=fetched, dataAgeSeconds=age, error=None,
                              throughput=None if rate is None else round(rate, 1),
                              throughputMinutes=round(minutes, 1))
            else:
                fails += 1
                if _state["ok"] or _state["error"] != err:  # log each new failure once
                    print("%s read failed: %s" % (datetime.now().strftime("%Y-%m-%d %I:%M:%S %p"), err), flush=True)
                _state.update(ok=False, error=err)
        if time.monotonic() - since >= LOG_EVERY:
            print("%s reads=%d failed=%d read_s mean=%.3f max=%.3f last=%.3f" % (
                datetime.now().strftime("%Y-%m-%d %I:%M:%S %p"), len(times), fails,
                sum(times) / len(times), max(times), took), flush=True)
            times, fails, since = [], 0, time.monotonic()
        wait = MIN_INTERVAL - (time.monotonic() - start)
        if wait > 0:
            time.sleep(wait)


class Handler(BaseHTTPRequestHandler):
    def _send(self, code, body, ctype):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store, max-age=0")
        self.send_header("Pragma", "no-cache")
        self.send_header("Expires", "0")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/api/sprint":
            with _lock:
                body = json.dumps(dict(_state, build=build_id())).encode()
            return self._send(200, body, "application/json")
        if path in ("/", "/index.html"):
            return self._send(200, index_page(), "text/html; charset=utf-8")
        if path in ("/logo-tile-192.png", "/logo-tile-384.png"):
            f = tile_copy(int(path[11:-4]))
            if not f:
                return self._send(404, b"no tile logo\n", "text/plain")
            with open(f, "rb") as fh:
                body = fh.read()
            return self._send(200, body, image_type(body))
        if path in ("/logo-icon.png", "/favicon.png"):
            f = derived("icon" if path == "/logo-icon.png" else "favicon")
            if not f:
                return self._send(404, b"no raster logo\n", "text/plain")
            with open(f, "rb") as fh:
                body = fh.read()
            return self._send(200, body, image_type(body))
        if path in ("/logo.webp", "/logo.png") and os.path.exists(os.path.join(ROOT, path[1:])):
            with open(os.path.join(ROOT, path[1:]), "rb") as fh:
                return self._send(200, fh.read(), "image/webp" if path.endswith("webp") else "image/png")
        if path == "/favicon.svg":
            body = favicon()
            if body is None:
                return self._send(404, b"no logo.svg\n", "text/plain")
            return self._send(200, body, "image/svg+xml")
        if path == "/healthz":
            return self._send(200, b"ok\n", "text/plain")
        if path in STATIC:
            name, ctype = STATIC[path]
            with open(os.path.join(ROOT, name), "rb") as f:
                return self._send(200, f.read(), ctype)
        self._send(404, b"not found\n", "text/plain")

    do_HEAD = do_GET

    def log_message(self, fmt, *args):
        pass  # quiet: the page polls every few seconds


def main():
    threading.Thread(target=poller, daemon=True).start()
    srv = ThreadingHTTPServer((HOST, PORT), Handler)
    print("sprint dashboard on %s:%d, reads back to back, min interval %ss" % (HOST, PORT, MIN_INTERVAL), flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
