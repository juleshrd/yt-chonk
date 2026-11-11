#!/bin/sh
set -eu

# Run from any directory. Existing archives of the same version are replaced.
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
version=${1:-dev}
case "$version" in
  ''|*[!A-Za-z0-9._-]*) printf '%s\n' 'Invalid version (use letters, digits, dots, hyphens and underscores).' >&2; exit 2 ;;
esac
mkdir -p dist
stage=$(mktemp -d "${TMPDIR:-/tmp}/yt-chonk-release.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
sums="$stage/SHA256SUMS"
: > "$sums"
for target_os in darwin linux; do
  for target_arch in amd64 arm64; do
    name="yt-chonk_${version}_${target_os}_${target_arch}"
    mkdir "$stage/$name"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
      go build -trimpath -ldflags "-X yt-chonk/internal/app.Version=$version" -o "$stage/$name/yt-chonk" ./cmd/yt-chonk
    cp README.md LICENSE "$stage/$name/"
    tar -czf "dist/$name.tar.gz" -C "$stage" "$name"
    if command -v sha256sum >/dev/null 2>&1; then
      (cd dist && sha256sum "$name.tar.gz") >> "$sums"
    else
      (cd dist && shasum -a 256 "$name.tar.gz") >> "$sums"
    fi
  done
done
cp "$sums" dist/SHA256SUMS
printf '%s\n' 'Archives and SHA256SUMS are available in dist/.'
