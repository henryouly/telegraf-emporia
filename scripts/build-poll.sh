#!/bin/sh
# Cross-compile emporia-poll for a Linux host.
# Usage: ./scripts/build-poll.sh [GOARCH]   (default: amd64; e.g. arm64 for Raspberry Pi)
# Output: dist/emporia-poll-linux-<GOARCH>
set -eu

ARCH="${1:-amd64}"
OUT="dist/emporia-poll-linux-${ARCH}"

mkdir -p dist
GOOS=linux GOARCH="$ARCH" go build -ldflags="-s -w" -o "$OUT" ./cmd/poll
file "$OUT"
