package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const domExtractJS = `(() => {
  const out = [];
  const seen = new Set();
  const vis = (el) => {
    if (!el || el.hidden) return false;
    const s = getComputedStyle(el);
    if (s.display === 'none' || s.visibility === 'hidden') return false;
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  };
  const cellText = (el) => {
    const kids = Array.from(el.children).filter(vis);
    if (kids.length > 0) {
      return kids.map(c => (c.innerText || c.textContent || '').replace(/\s+/g, ' ').trim()).filter(Boolean).join(' ');
    }
    return (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim();
  };
  const addEl = (label, el, structural, useCells) => {
    if (!el || seen.has(el) || !vis(el)) return;
    seen.add(el);
    const r = el.getBoundingClientRect();
    const txt = useCells ? cellText(el) : (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim();
    out.push({
      label,
      x: Math.max(0, Math.floor(r.left + window.scrollX)),
      y: Math.max(0, Math.floor(r.top + window.scrollY)),
      w: Math.ceil(r.width),
      h: Math.ceil(r.height),
      text: txt,
      structural: !!structural
    });
  };
  const addGroup = (label, els, structural) => {
    const list = Array.from(els || []).filter(vis);
    if (list.length === 0) return;
    list.forEach(e => seen.add(e));
    let x1 = Infinity, y1 = Infinity, x2 = 0, y2 = 0;
    const parts = [];
    for (const el of list) {
      const r = el.getBoundingClientRect();
      x1 = Math.min(x1, Math.floor(r.left + window.scrollX));
      y1 = Math.min(y1, Math.floor(r.top + window.scrollY));
      x2 = Math.max(x2, Math.ceil(r.right + window.scrollX));
      y2 = Math.max(y2, Math.ceil(r.bottom + window.scrollY));
      const t = cellText(el);
      if (t) parts.push(t);
    }
    out.push({ label, x: Math.max(0, x1), y: Math.max(0, y1), w: Math.max(1, x2 - x1), h: Math.max(1, y2 - y1), text: parts.join(' | '), structural: !!structural });
  };
  document.querySelectorAll('[data-region]').forEach(el => addEl(el.getAttribute('data-region'), el, true, true));
  addEl('Top bar', document.querySelector('.top'), false, false);
  document.querySelectorAll('.hero .tile').forEach(t => {
    const lbl = t.querySelector('.label');
    const name = lbl ? lbl.textContent.trim() : 'tile';
    addEl('Hero: ' + name, t, false, false);
  });
  addEl('Cost breakdown header', document.querySelector('section.panel.spend .panel-head'), true, false);
  addEl('Cost breakdown body', document.querySelector('section.panel.spend .spend-body'), false, false);
  addEl('Cards by state', document.querySelector('section.progress-card'), false, false);
  addEl('Work panel header', document.querySelector('section.panel.streams .panel-head'), false, false);
  addEl('Work table header', document.querySelector('#streams .row.head'), true, true);
  addGroup('Work table rows', document.querySelectorAll('#streams .row:not(.head)'), false);
  addEl('Fleet panel header', document.querySelector('section.panel.fleet .panel-head'), false, false);
  addEl('Fleet table header', document.querySelector('#fleet .row.head'), true, true);
  addGroup('Fleet table rows', document.querySelectorAll('#fleet .row:not(.head)'), false);
  addEl('Friends panel header', document.querySelector('section.panel.friends .panel-head'), false, false);
  addEl('Friends table header', document.querySelector('#friends .row.head'), true, true);
  addGroup('Friends table rows', document.querySelectorAll('#friends .row:not(.head)'), false);
  addEl('Providers panel', document.querySelector('#providers-panel'), true, false);
  addEl('Readers and merge', document.querySelector('#readers-panel'), true, false);
  addEl('Landings chart', document.querySelector('#landings-panel'), true, false);
  addEl('Last 24 hours', document.querySelector('#history-panel'), true, false);
  addEl('Footer', document.querySelector('footer'), false, false);
  if (!document.querySelector('#streams')) {
    document.querySelectorAll('header, nav, section, table thead, table tbody, h1, h2').forEach(el => {
      const id = el.getAttribute('aria-label') || el.id || el.tagName.toLowerCase();
      addEl(id, el, el.tagName === 'THEAD' || el.tagName === 'H1' || el.tagName === 'H2', true);
    });
  }
  return out;
})()`

func findChrome(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("ABSHOT_CHROME"); env != "" {
		return env, nil
	}
	for _, name := range []string{"chrome-headless-shell", "google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	home, _ := os.UserHomeDir()
	patterns := []string{
		filepath.Join(home, "sdk/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell"),
		filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell"),
		filepath.Join(home, "sdk/ms-playwright/chromium-*/chrome-linux64/chrome"),
		filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux64/chrome"),
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		if len(matches) > 0 {
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			return matches[0], nil
		}
	}
	return "", errors.New("headless chrome/chromium binary not found; set ABSHOT_CHROME")
}

type cdpConn struct {
	conn   net.Conn
	reader *bufio.Reader
	nextID int
}

func capturePage(ctx context.Context, pageURL string, width int, settle time.Duration, tz, chromeBin string) (image.Image, []DOMRegion, error) {
	tmpDir, err := os.MkdirTemp("", "abshot-chrome-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmpDir)
	cmd, wsURL, err := startChrome(ctx, chromeBin, tz, tmpDir)
	if err != nil {
		return nil, nil, err
	}
	defer stopChrome(cmd)
	sess, err := dialWS(ctx, wsURL)
	if err != nil {
		return nil, nil, err
	}
	defer sess.conn.Close()
	return runCaptureSession(ctx, sess, pageURL, width, settle, tz)
}

func startChrome(ctx context.Context, chromeBin, tz, tmpDir string) (*exec.Cmd, string, error) {
	args := []string{
		"--headless", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
		"--remote-debugging-port=0", "--remote-allow-origins=*",
		"--user-data-dir=" + tmpDir, "about:blank",
	}
	cmd := exec.CommandContext(ctx, chromeBin, args...)
	cmd.Env = append(os.Environ(), "TZ="+tz)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	port, err := waitDevToolsPort(ctx, stderr)
	if err != nil {
		stopChrome(cmd)
		return nil, "", err
	}
	wsURL, err := pageWebSocketURL(ctx, port)
	if err != nil {
		stopChrome(cmd)
		return nil, "", err
	}
	return cmd, wsURL, nil
}

func stopChrome(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

var devToolsRe = regexp.MustCompile(`DevTools listening on ws://127\.0\.0\.1:(\d+)/`)

func waitDevToolsPort(ctx context.Context, r io.Reader) (string, error) {
	type res struct {
		port string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			if m := devToolsRe.FindStringSubmatch(sc.Text()); len(m) == 2 {
				ch <- res{port: m[1]}
				return
			}
		}
		ch <- res{err: errors.New("chrome exited before reporting DevTools port")}
	}()
	select {
	case out := <-ch:
		return out.port, out.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(15 * time.Second):
		return "", errors.New("timed out waiting for chrome DevTools port")
	}
}

func pageWebSocketURL(ctx context.Context, port string) (string, error) {
	endpoint := "http://127.0.0.1:" + port + "/json/list"
	deadline := time.Now().Add(10 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if ws := tryFetchPageWS(ctx, client, endpoint); ws != "" {
			return ws, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", errors.New("timed out waiting for chrome page target")
}

func tryFetchPageWS(ctx context.Context, client *http.Client, endpoint string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var targets []struct {
		Type                 string `json:"type"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return ""
	}
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL
		}
	}
	return ""
}

func runCaptureSession(ctx context.Context, sess *cdpConn, pageURL string, width int, settle time.Duration, tz string) (image.Image, []DOMRegion, error) {
	if _, err := sess.call(ctx, "Page.enable", map[string]any{}); err != nil {
		return nil, nil, err
	}
	if _, err := sess.call(ctx, "Runtime.enable", map[string]any{}); err != nil {
		return nil, nil, err
	}
	if _, err := sess.call(ctx, "Emulation.setTimezoneOverride", map[string]any{"timezoneId": tz}); err != nil {
		return nil, nil, fmt.Errorf("timezone override %q: %w", tz, err)
	}
	metrics := map[string]any{"width": width, "height": 1000, "deviceScaleFactor": 1, "mobile": false}
	if _, err := sess.call(ctx, "Emulation.setDeviceMetricsOverride", metrics); err != nil {
		return nil, nil, err
	}
	if _, err := sess.call(ctx, "Page.navigate", map[string]any{"url": pageURL}); err != nil {
		return nil, nil, err
	}
	if err := waitReadyAndSettle(ctx, sess, settle); err != nil {
		return nil, nil, err
	}
	fullH, err := measureFullHeight(ctx, sess)
	if err != nil {
		return nil, nil, err
	}
	metrics["height"] = fullH
	if _, err := sess.call(ctx, "Emulation.setDeviceMetricsOverride", metrics); err != nil {
		return nil, nil, err
	}
	time.Sleep(100 * time.Millisecond)
	dom, err := evalDOMRegions(ctx, sess)
	if err != nil {
		return nil, nil, err
	}
	img, err := grabScreenshot(ctx, sess)
	return img, dom, err
}

func waitReadyAndSettle(ctx context.Context, sess *cdpConn, settle time.Duration) error {
	waitExpr := `new Promise((resolve) => {
		const start = Date.now();
		const check = () => {
			if (window.location.href === 'about:blank' || document.readyState !== 'complete') {
				return setTimeout(check, 40);
			}
			const hasStreams = !!document.getElementById('streams');
			const hasLanded = !!document.getElementById('landed');
			const rowsReady = !hasStreams || !hasLanded ||
				document.querySelectorAll('#streams .row:not(.head)').length > 0 ||
				(Date.now() - start > 5000);
			if (!rowsReady) {
				return setTimeout(check, 50);
			}
			(document.fonts ? document.fonts.ready : Promise.resolve()).then(() => resolve(true));
		};
		check();
	})`
	_, err := sess.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    waitExpr,
		"awaitPromise":  true,
		"returnByValue": true,
	})
	if err != nil {
		return err
	}
	select {
	case <-time.After(settle):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func measureFullHeight(ctx context.Context, sess *cdpConn) (int, error) {
	expr := `Math.max(1000, Math.ceil(Math.max(document.documentElement.scrollHeight, document.body ? document.body.scrollHeight : 0)))`
	raw, err := sess.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
	})
	if err != nil {
		return 0, err
	}
	var res struct {
		Result struct {
			Value float64 `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, err
	}
	h := int(res.Result.Value)
	if h < 1000 {
		h = 1000
	}
	return h, nil
}

func evalDOMRegions(ctx context.Context, sess *cdpConn) ([]DOMRegion, error) {
	raw, err := sess.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    domExtractJS,
		"returnByValue": true,
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		Result struct {
			Value []DOMRegion `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return res.Result.Value, nil
}

func grabScreenshot(ctx context.Context, sess *cdpConn) (image.Image, error) {
	raw, err := sess.call(ctx, "Page.captureScreenshot", map[string]any{
		"format":      "png",
		"fromSurface": true,
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	pngBytes, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(pngBytes))
}

func dialWS(ctx context.Context, rawURL string) (*cdpConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	rand.Read(keyBytes) // crypto/rand.Read never returns an error (it crashes the program instead)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		u.RequestURI(), u.Host, key)
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 64*1024)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake failed: %s %v", strings.TrimSpace(statusLine), err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	return &cdpConn{conn: conn, reader: br}, nil
}

func (c *cdpConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("cdp %s: set deadline: %w", method, err)
	}
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := wsSend(c.conn, 0x1, payload); err != nil {
		return nil, err
	}
	for {
		msg, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		var env struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(msg, &env); err != nil || env.ID != id {
			continue
		}
		if env.Error != nil {
			return nil, errors.New(env.Error.Message)
		}
		return env.Result, nil
	}
}

func wsSend(w io.Writer, opcode byte, payload []byte) error {
	var hdr bytes.Buffer
	hdr.WriteByte(0x80 | opcode)
	n := len(payload)
	switch {
	case n <= 125:
		hdr.WriteByte(0x80 | byte(n))
	case n <= 65535:
		hdr.WriteByte(0x80 | 126)
		hdr.Write(binary.BigEndian.AppendUint16(nil, uint16(n)))
	default:
		hdr.WriteByte(0x80 | 127)
		hdr.Write(binary.BigEndian.AppendUint64(nil, uint64(n)))
	}
	var mask [4]byte
	rand.Read(mask[:]) // crypto/rand.Read never returns an error
	hdr.Write(mask[:])
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i&3]
	}
	if _, err := w.Write(hdr.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(masked)
	return err
}

func (c *cdpConn) readMessage() ([]byte, error) {
	var full bytes.Buffer
	for {
		fin, op, data, err := wsReadFrame(c.reader)
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x9:
			if err := wsSend(c.conn, 0xA, data); err != nil {
				return nil, fmt.Errorf("cdp: answering a ping: %w", err)
			}
			continue
		case 0xA:
			continue
		case 0x8:
			return nil, io.EOF
		}
		full.Write(data)
		if fin {
			return full.Bytes(), nil
		}
	}
}

func wsReadFrame(r *bufio.Reader) (bool, byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin := (h[0] & 0x80) != 0
	op := h[0] & 0x0f
	masked := (h[1] & 0x80) != 0
	length := uint64(h[1] & 0x7f)
	switch length {
	case 126:
		var ext uint16
		if err := binary.Read(r, binary.BigEndian, &ext); err != nil {
			return false, 0, nil, err
		}
		length = uint64(ext)
	case 127:
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return false, 0, nil, err
		}
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range data {
			data[i] ^= mask[i&3]
		}
	}
	return fin, op, data, nil
}
