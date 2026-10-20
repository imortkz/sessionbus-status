#!/bin/sh
# SPDX-License-Identifier: MIT
# Offline test of scripts/install.sh: file:// download root plus a fake curl for "latest".
set -eu

repo=$(CDPATH='' cd "$(dirname "$0")/.." && pwd -P)
work=$(mktemp -d)
trap 'rm -rf "$work"' 0
trap 'exit 1' HUP INT TERM

fail() { echo "FAIL: $*" >&2; exit 1; }
sums() { (cd "$1" && if command -v sha256sum >/dev/null; then sha256sum "$2"; else shasum -a 256 "$2"; fi) > "$1/SHA256SUMS"; }

goos=$(go env GOOS)
goarch=$(go env GOARCH)
asset="sessionbus-status-$goos-$goarch.tar.gz"

good="$work/good"
SESSIONBUS_STATUS_RELEASE=v$(cat "$repo/RELEASE_VERSION") "$repo/scripts/package" "$good" >/dev/null
sums "$good" "$asset"

# positive: pinned version from a file:// root
home="$work/home"
mkdir "$home"
HOME=$home SESSIONBUS_STATUS_VERSION=v0.1.0-alpha.1 SESSIONBUS_STATUS_DOWNLOAD_ROOT="file://$good" sh "$repo/scripts/install.sh" >/dev/null ||
  fail 'install from file root'
[ -x "$home/.local/libexec/sessionbus/sessionbus-status/sessionbus-status" ] || fail 'binary not installed'
[ -L "$home/.local/bin/sessionbus-status" ] || fail 'symlink missing'
if [ -f "$repo/version.go" ]; then
  "$home/.local/bin/sessionbus-status" --version | grep -Eq '^sessionbus-status v0\.1\.0-alpha\.1 \([0-9a-f]{40}\)$' || fail '--version output'
fi

expect_fail() { # name, message-substring, env... -- runs install.sh with HOME=$work/neg
  name=$1; want=$2; shift 2
  rm -rf "$work/neg"; mkdir "$work/neg"
  if out=$(env HOME="$work/neg" "$@" sh "$repo/scripts/install.sh" 2>&1); then fail "$name: expected failure"; fi
  printf '%s' "$out" | grep -q "$want" || fail "$name: message '$out' lacks '$want'"
  [ ! -e "$work/neg/.local/bin/sessionbus-status" ] || fail "$name: installed despite failure"
}

# bad checksum
bad="$work/bad"; mkdir "$bad"; cp "$good/$asset" "$bad/"
printf '%064d  %s\n' 0 "$asset" > "$bad/SHA256SUMS"
expect_fail 'bad checksum' 'checksum mismatch' SESSIONBUS_STATUS_VERSION=v0.1.0-alpha.1 SESSIONBUS_STATUS_DOWNLOAD_ROOT="file://$bad"

# bad version strings and download root
expect_fail 'bad version' 'Invalid SESSIONBUS_STATUS_VERSION' SESSIONBUS_STATUS_VERSION=1.2
expect_fail 'bad version chars' 'Invalid SESSIONBUS_STATUS_VERSION' 'SESSIONBUS_STATUS_VERSION=v1.0.0;x'
expect_fail 'http root' 'must use https:// or file://' SESSIONBUS_STATUS_DOWNLOAD_ROOT=http://example.invalid

# archive with a ../ member
evil="$work/evil"; mkdir -p "$evil/src/sub"
printf 'x\n' > "$evil/src/payload"
(cd "$evil/src/sub" && tar -czPf "$evil/$asset" ../payload) || fail 'cannot build evil archive'
tar -tzf "$evil/$asset" | grep -q '^\.\./' || fail 'evil archive lacks ../ member'
sums "$evil" "$asset"
expect_fail 'unsafe member' 'Unsafe archive member' SESSIONBUS_STATUS_VERSION=v0.1.0-alpha.1 SESSIONBUS_STATUS_DOWNLOAD_ROOT="file://$evil"

# symlink member
link="$work/link"; mkdir -p "$link/src"
printf 'sessionbus-status\n' > "$link/src/ROLE"
ln -s /bin/sh "$link/src/install"
tar -czf "$link/$asset" -C "$link/src" ROLE install
sums "$link" "$asset"
expect_fail 'symlink member' 'Unsafe archive member' SESSIONBUS_STATUS_VERSION=v0.1.0-alpha.1 SESSIONBUS_STATUS_DOWNLOAD_ROOT="file://$link"

# wrong role
role="$work/role"; mkdir -p "$role/src"
printf 'other\n' > "$role/src/ROLE"
printf '#!/bin/sh\nexit 0\n' > "$role/src/install"
tar -czf "$role/$asset" -C "$role/src" ROLE install
sums "$role" "$asset"
expect_fail 'wrong role' 'Wrong release role' SESSIONBUS_STATUS_VERSION=v0.1.0-alpha.1 SESSIONBUS_STATUS_DOWNLOAD_ROOT="file://$role"

# latest: fake curl serves the releases list; gh is reported unauthenticated
fakebin="$work/fakebin"; mkdir "$fakebin"
cat > "$fakebin/gh" <<'SH'
#!/bin/sh
exit 1
SH
cat > "$fakebin/curl" <<'SH'
#!/bin/sh
# usage: curl ... URL [-o FILE]; serves $FAKE_RELEASES for the API and $FAKE_ASSETS/<tag>/<file> for downloads
url=; out=
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2;;
    http*) url=$1; shift;;
    *) shift;;
  esac
done
case "$url" in
  https://api.github.com/repos/imortkz/sessionbus-status/releases*) cat "$FAKE_RELEASES";;
  https://github.com/imortkz/sessionbus-status/releases/download/*)
    rest=${url#https://github.com/imortkz/sessionbus-status/releases/download/}
    echo "$rest" >> "$FAKE_LOG"
    cp "$FAKE_ASSETS/$rest" "$out";;
  *) exit 22;;
esac
SH
chmod +x "$fakebin/gh" "$fakebin/curl"
cat > "$work/releases.json" <<'JSON'
[
  {"tag_name": "v0.1.0-alpha.2", "draft": false, "prerelease": true},
  {"tag_name": "v0.1.0-alpha.10", "draft": false, "prerelease": true},
  {"tag_name": "v0.0.9", "draft": false, "prerelease": false},
  {"tag_name": "v0.2.0", "draft": true, "prerelease": false},
  {"tag_name": "nightly", "draft": false, "prerelease": true}
]
JSON
assets="$work/assets"; mkdir -p "$assets/v0.1.0-alpha.10"
cp "$good/$asset" "$good/SHA256SUMS" "$assets/v0.1.0-alpha.10/"
: > "$work/fetched"
rm -rf "$work/neg"; mkdir "$work/neg"
PATH="$fakebin:$PATH" HOME="$work/neg" FAKE_RELEASES="$work/releases.json" FAKE_ASSETS="$assets" FAKE_LOG="$work/fetched" \
  sh "$repo/scripts/install.sh" >/dev/null || fail 'latest install'
grep -qx "v0.1.0-alpha.10/$asset" "$work/fetched" || fail "latest picked wrong tag: $(cat "$work/fetched")"
[ -L "$work/neg/.local/bin/sessionbus-status" ] || fail 'latest: symlink missing'

# latest from compact (single-line) JSON: draft skipped, semver max picked
printf '%s' '[{"tag_name":"v0.1.0-alpha.3","draft":true,"prerelease":true},{"tag_name":"v0.1.0-alpha.2","draft":false,"prerelease":true},{"tag_name":"v0.1.0-alpha.1","draft":false,"prerelease":true}]' > "$work/compact.json"
mkdir -p "$assets/v0.1.0-alpha.2"
cp "$good/$asset" "$good/SHA256SUMS" "$assets/v0.1.0-alpha.2/"
: > "$work/fetched"
rm -rf "$work/neg"; mkdir "$work/neg"
PATH="$fakebin:$PATH" HOME="$work/neg" FAKE_RELEASES="$work/compact.json" FAKE_ASSETS="$assets" FAKE_LOG="$work/fetched" \
  sh "$repo/scripts/install.sh" >/dev/null || fail 'latest install (compact JSON)'
grep -qx "v0.1.0-alpha.2/$asset" "$work/fetched" || fail "compact latest picked wrong tag: $(cat "$work/fetched")"

# pin without leading v is normalised to the tag
: > "$work/fetched"
rm -rf "$work/neg"; mkdir "$work/neg"
PATH="$fakebin:$PATH" HOME="$work/neg" SESSIONBUS_STATUS_VERSION=0.1.0-alpha.2 FAKE_ASSETS="$assets" FAKE_LOG="$work/fetched" \
  sh "$repo/scripts/install.sh" >/dev/null || fail 'pin without v'
grep -qx "v0.1.0-alpha.2/$asset" "$work/fetched" || fail "pin without v fetched: $(cat "$work/fetched")"

echo 'install_test: ok'
