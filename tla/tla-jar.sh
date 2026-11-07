# The pinned TLC jar, sourced by tla/check-trace.sh and tla/ci-tlc.sh (not run):
#   . "$HERE/tla-jar.sh"; tla_jar_ensure "$CACHE" || exit 1; JAR="$TLA_JAR"
# Upstream replaced its v1.8.0 tla2tools.jar, so the pin is this repository's copy
# in release tla-tools-1.8.0. TLA_SHA256 is that asset's sha256 (its "digest" in the
# releases API). A jar that does not match is removed and refused, never run.
TLA_VERSION=v1.8.0
TLA_URL="https://github.com/mas-bandwidth/nova-sprint/releases/download/tla-tools-1.8.0/tla2tools.jar"
TLA_SHA256=7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04

tla_sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
    else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# tla_jar_ensure CACHE: leaves the verified jar at $TLA_JAR (CACHE/tla2tools-$TLA_VERSION.jar),
# downloading it when the cache has none or one that does not match. Returns 1 on a
# failed download or a checksum mismatch.
tla_jar_ensure() {
    local got
    TLA_JAR="$1/tla2tools-$TLA_VERSION.jar"
    mkdir -p "$1" || return 1
    if [ ! -f "$TLA_JAR" ] || [ "$(tla_sha256_of "$TLA_JAR")" != "$TLA_SHA256" ]; then
        curl -fsSL -o "$TLA_JAR.part.$$" "$TLA_URL" && mv -f "$TLA_JAR.part.$$" "$TLA_JAR" ||
            { rm -f "$TLA_JAR.part.$$"; echo "download of $TLA_URL failed" >&2; return 1; }
    fi
    got="$(tla_sha256_of "$TLA_JAR")"
    if [ "$got" != "$TLA_SHA256" ]; then
        echo "tla2tools.jar sha256 is $got, want $TLA_SHA256" >&2
        rm -f "$TLA_JAR"
        return 1
    fi
}
