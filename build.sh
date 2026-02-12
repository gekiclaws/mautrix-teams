#!/bin/sh
set -eu

if go tool maubuild -h >/dev/null 2>&1; then
    BINARY_NAME=mautrix-teams go tool maubuild "$@"
else
    echo "maubuild not available, falling back to go build" >&2
    go build -o mautrix-teams ./cmd/mautrix-teams
fi
