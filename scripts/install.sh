#!/bin/sh
# SPDX-License-Identifier: MIT
set -eu

repo=imortkz/sessionbus-status
version=${SESSIONBUS_STATUS_VERSION:-latest}
grammar='^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
case "$version" in ''|*[!A-Za-z0-9._-]*) echo 'Invalid SESSIONBUS_STATUS_VERSION' >&2; exit 1;; esac
if [ "$version" != latest ]; then
  printf '%s\n' "$version" | grep -Eq "$grammar" || { echo 'Invalid SESSIONBUS_STATUS_VERSION' >&2; exit 1; }
fi
case "$(uname -s)" in Linux) platform=linux;; Darwin) platform=darwin;; *) echo 'Supported systems: Linux and macOS' >&2; exit 1;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo 'Supported architectures: amd64 and arm64' >&2; exit 1;; esac
for cmd in tar awk mktemp grep sed sort; do command -v "$cmd" >/dev/null || { echo "Required command: $cmd" >&2; exit 1; }; done
if command -v sha256sum >/dev/null; then hash=sha256sum
elif command -v shasum >/dev/null; then hash=shasum
else echo 'Required command: sha256sum or shasum' >&2; exit 1; fi
asset="sessionbus-status-$platform-$arch.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' 0
trap 'exit 1' HUP INT TERM

# Highest semver tag among non-draft releases, prereleases included.
# Reads the releases JSON on stdin; prints one tag.
latest_tag() {
  tr '{}[]' ',,,,' | tr ',' '\n' | awk '
    /"tag_name"[[:space:]]*:/ { t=$0; sub(/.*"tag_name"[[:space:]]*:[[:space:]]*"/, "", t); sub(/".*/, "", t); tag=t }
    /"draft"[[:space:]]*:[[:space:]]*true/ { draft[tag]=1 }
    { if (tag != "") seen[tag]=1 }
    END { for (t in seen) if (!(t in draft)) print t }
  ' | grep -E "$grammar" | sed -E 's/^v//' | awk '
    # key: major minor patch, release-above-prerelease flag, prerelease with numeric parts zero-padded
    { core=$0; pre=""; i=index($0, "-"); if (i) { core=substr($0,1,i-1); pre=substr($0,i+1) }
      split(core, c, ".")
      padded=""; rest=pre
      while (match(rest, /[0-9]+/)) {
        padded=padded substr(rest,1,RSTART-1) sprintf("%09d", substr(rest,RSTART,RLENGTH)+0)
        rest=substr(rest,RSTART+RLENGTH)
      }
      padded=padded rest
      printf "%09d %09d %09d %d %s %s\n", c[1], c[2], c[3], (pre==""?1:0), (padded==""?"-":padded), $0 }
  ' | sort -k1,1 -k2,2 -k3,3 -k4,4n -k5,5 | tail -n 1 | awk '{print "v" $NF}'
}

list_releases() {
  if command -v gh >/dev/null && gh auth status >/dev/null 2>&1; then
    gh api "repos/$repo/releases?per_page=20"
  else
    command -v curl >/dev/null || { echo 'Required command: curl' >&2; return 1; }
    curl -fsSL --retry 2 --connect-timeout 10 --max-time 30 "https://api.github.com/repos/$repo/releases?per_page=20"
  fi
}

if [ -n "${SESSIONBUS_STATUS_DOWNLOAD_ROOT:-}" ]; then
  case "$SESSIONBUS_STATUS_DOWNLOAD_ROOT" in https://*|file://*) ;; *) echo 'SESSIONBUS_STATUS_DOWNLOAD_ROOT must use https:// or file://' >&2; exit 1;; esac
  base=${SESSIONBUS_STATUS_DOWNLOAD_ROOT%/}
else
  if [ "$version" = latest ]; then
    list_releases > "$tmp/releases" || { echo "Cannot list releases of $repo; check the network or set SESSIONBUS_STATUS_DOWNLOAD_ROOT" >&2; exit 1; }
    tag=$(latest_tag < "$tmp/releases")
    [ -n "$tag" ] || { echo "No release found for $repo" >&2; exit 1; }
  else
    tag=v${version#v}
  fi
  base="https://github.com/$repo/releases/download/$tag"
fi

command -v curl >/dev/null || { echo 'Required command: curl' >&2; exit 1; }
curl -fLsS --retry 2 "$base/$asset" -o "$tmp/$asset" || { echo "Cannot download $repo release; check the network or set SESSIONBUS_STATUS_DOWNLOAD_ROOT" >&2; exit 1; }
curl -fLsS --retry 2 "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" || { echo "Cannot download $repo checksums; check the network or set SESSIONBUS_STATUS_DOWNLOAD_ROOT" >&2; exit 1; }

expected=$(awk -v file="$asset" '
  $2 == file { count++; if (NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-f]/) bad=1; value=$1 }
  END { if (count != 1 || bad) exit 1; print value }
' "$tmp/SHA256SUMS") || { echo 'Missing, invalid or ambiguous checksum' >&2; exit 1; }
if [ "$hash" = sha256sum ]; then actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}'); fi
[ "$actual" = "$expected" ] || { echo 'Release checksum mismatch' >&2; exit 1; }
tar -tzf "$tmp/$asset" > "$tmp/members"
tar -tvzf "$tmp/$asset" > "$tmp/listing"
if awk '/^\// || /(^|\/)\.\.(\/|$)/ {bad=1} END {exit !bad}' "$tmp/members" ||
  awk '!/^[-d]/ {bad=1} END {exit !bad}' "$tmp/listing" ||
  [ "$(awk 'END {print NR}' "$tmp/members")" != "$(awk 'END {print NR}' "$tmp/listing")" ]; then echo 'Unsafe archive member' >&2; exit 1; fi
mkdir "$tmp/payload"
tar -xzf "$tmp/$asset" -C "$tmp/payload"
[ "$(cat "$tmp/payload/ROLE")" = sessionbus-status ] || { echo 'Wrong release role' >&2; exit 1; }
sh "$tmp/payload/install" </dev/null
