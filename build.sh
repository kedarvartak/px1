#!/usr/bin/env sh
# Build px1 for every supported platform into dist/.
set -eu

VERSION=$(cat VERSION | tr -d ' \r\n')
OUT=${OUT:-dist}
POSTHOG_KEY="${POSTHOG_KEY:-${PX1_POSTHOG_KEY:-}}"

LDFLAGS="-s -w"
if [ -n "$POSTHOG_KEY" ]; then
  LDFLAGS="$LDFLAGS -X main.posthogKey=$POSTHOG_KEY"
fi

# Bundle frontend web assets
./scripts/build-web.js

TARGETS="
linux/amd64 linux/arm64 linux/arm linux/386 linux/riscv64
darwin/amd64 darwin/arm64
windows/amd64 windows/arm64 windows/386
freebsd/amd64 freebsd/arm64 openbsd/amd64 openbsd/arm64 netbsd/amd64
"

rm -rf "$OUT"
mkdir -p "$OUT"

for t in $TARGETS; do
  os=${t%/*}
  arch=${t#*/}
  ext=""
  [ "$os" = "windows" ] && ext=".exe"
  name="px1-$VERSION-$os-$arch$ext"
  printf '  %-28s' "$name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$OUT/$name" .
  printf '%s\n' "$(du -h "$OUT/$name" | cut -f1)"
done

echo "built $(ls -1 "$OUT" | wc -l | tr -d ' ') binaries in $OUT/"
