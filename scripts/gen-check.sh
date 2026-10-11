#!/usr/bin/env bash
# Generator check: scaffold a full service with every `skit add` module and
# their variants against this checkout, then run the service's `make check`
# (lint, project rules, govulncheck, tests). Needs buf and network access.
# The service is removed on success and kept for inspection on failure.
#
# Usage: gen-check.sh
set -euo pipefail

sdk=$(pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/skit-gen-check.XXXXXX")
svc="$tmp/sample"
skit="$tmp/skit"

go build -o "$skit" ./cmd/skit
"$skit" new github.com/acme/sample --dir "$svc" --full --replace "$sdk" > /dev/null
cd "$svc"

add() {
	echo ">> skit add $*"
	"$skit" add "$@" > /dev/null
}
add rest widget
add rest label --no-tests
add rest-test label
add grpc gadget
add grpc-test gadget
add consumer order
add consumer payment --broker kafka
add consumer audit --broker nats
add worker cleanup
add worker mailer --claim
add cron report
add cron digest --lock postgres
add cron sync --lock redis
add event widget-created --with-relay
add event order-placed
add http-client billing
add http-client identity --oauth
add migration add-index

make proto > /dev/null
go mod tidy
go generate ./...
make check

cd "$sdk"
rm -rf "$tmp"
echo "gen-check: OK"
