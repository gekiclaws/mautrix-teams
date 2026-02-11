#!/bin/sh
set -e

# Build the bridgev2 entrypoint.
go build -o mautrix-teams \
  -ldflags "-X main.Tag=$(git describe --exact-match --tags 2>/dev/null) -X main.Commit=$(git rev-parse HEAD) -X 'main.BuildTime=`date '+%b %_d %Y, %H:%M:%S'`'" \
  "$@" \
  ./cmd/mautrix-teams
