#!/bin/sh
# Runs the load test against a piecemeal built from this checkout, with
# its data and downloads in a temporary directory, and prints the report.
#
#   test/load/run.sh                  1,000 torrents of 1 MiB
#   N=200 SIZE=4194304 test/load/run.sh
#   DIR=/tank/load test/load/run.sh   put the data on another disk, such as a ZFS pool
set -eu
cd "$(dirname "$0")/../.."

n=${N:-1000}
size=${SIZE:-1048576}
work=$(mktemp -d)
dir=${DIR:-$work/downloads}
port=${PORT:-18181}
password=load-test-password
trap 'kill "$pid" 2> /dev/null || true; wait "$pid" 2> /dev/null || true; rm -rf "$work"; [ -z "${DIR:-}" ] || rm -rf "$DIR"' EXIT

CGO_ENABLED=0 go build -o "$work/piecemeal" ./cmd/piecemeal
mkdir -p "$dir"
PIECEMEAL_ADMIN_PASSWORD_HASH=$(printf '%s\n' "$password" | "$work/piecemeal" hash-password) \
  PIECEMEAL_ADDR=127.0.0.1:$port \
  PIECEMEAL_DATA_DIR=$work/data \
  PIECEMEAL_DOWNLOAD_DIR=$dir \
  PIECEMEAL_SECURE_COOKIES=false \
  PIECEMEAL_METRICS=auth \
  PIECEMEAL_LOG_LEVEL=warn \
  PIECEMEAL_DHT=false \
  PIECEMEAL_QUEUEING_ENABLED=false \
  "$work/piecemeal" > "$work/piecemeal.log" 2>&1 &
pid=$!

base=http://127.0.0.1:$port
until curl -fs "$base/healthz" > /dev/null; do
  kill -0 "$pid" 2> /dev/null || { cat "$work/piecemeal.log"; exit 1; }
  sleep 0.2
done
curl -fs -c "$work/cookies" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$password\"}" "$base/api/piecemeal/v1/auth/login" > /dev/null
key=$(curl -fs -b "$work/cookies" -H 'Content-Type: application/json' -H 'X-Requested-With: piecemeal' \
  -d '{"name":"load test","scope":"full"}' "$base/api/piecemeal/v1/api-keys" |
  sed -n 's/.*"key":"\([^"]*\)".*/\1/p')

PIECEMEAL_API_KEY=$key go run ./test/load -url "$base" -dir "$dir/load" -n "$n" -size "$size"
