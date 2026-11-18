#!/bin/sh
# Runs the end-to-end tests: builds and starts the stack in compose.yaml,
# runs the Go tests tagged e2e against it, and tears it down.
#
#   test/e2e/run.sh            from the repository root
#   KEEP=1 test/e2e/run.sh     leave the stack running afterwards
set -eu
cd "$(dirname "$0")"

# go run needs the web UI's embed directory, which CI doesn't build here.
mkdir -p ../../internal/webui/dist
[ -n "$(ls -A ../../internal/webui/dist)" ] || touch ../../internal/webui/dist/.keep

password=e2e-admin-password
PIECEMEAL_ADMIN_PASSWORD_HASH=$(printf '%s\n' "$password" | CGO_ENABLED=0 go run ../../cmd/piecemeal hash-password)
export PIECEMEAL_ADMIN_PASSWORD_HASH

# Leftovers from a run with KEEP=1.
docker compose down -v --remove-orphans > /dev/null 2>&1 || true
rm -rf tmp
mkdir -p tmp/downloads tmp/shared tmp/movies
# A real video, short to transfer but long enough that Radarr doesn't take
# it for a sample: 20 minutes of black at 1 frame per second, with silence
# (Radarr refuses files without audio).
release=Big.Buck.Bunny.2008.1080p.BluRay.x264-PIECEMEAL
mkdir -p "tmp/seed/$release"
# As this user, so the file isn't root's on Linux hosts.
docker run --rm --user "$(id -u):$(id -g)" --entrypoint ffmpeg \
  -v "$PWD/tmp/seed/$release:/out" lscr.io/linuxserver/ffmpeg:9.0.2 \
  -hide_banner -loglevel error -f lavfi -i color=c=black:s=64x64:r=1 \
  -f lavfi -i anullsrc=r=8000:cl=mono -t 1200 \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p -c:a aac -b:a 8k "/out/$release.mkv"
# The *arrs get a known API key from a pre-seeded config.xml.
for app in radarr:7878 sonarr:8989 prowlarr:9696; do
  name=${app%%:*}
  port=${app#*:}
  mkdir -p "tmp/$name"
  cat > "tmp/$name/config.xml" <<XML
<Config>
  <BindAddress>*</BindAddress>
  <Port>$port</Port>
  <ApiKey>e2e0000000000000000000000000$port</ApiKey>
  <AuthenticationMethod>External</AuthenticationMethod>
  <AuthenticationRequired>DisabledForLocalAddresses</AuthenticationRequired>
  <AnalyticsEnabled>False</AnalyticsEnabled>
  <LogLevel>debug</LogLevel>
</Config>
XML
done
# VueTorrent, which piecemeal serves at /qbittorrent/.
# renovate: datasource=github-releases depName=VueTorrent/VueTorrent
vuetorrent=v2.37.0
vuetorrent_sha256=5244a29314a4befcc1ffb659e8b8b03f8c1a84b7a3d3f26a651bd6069b57a7ed
curl -fsSL -o tmp/vuetorrent.zip "https://github.com/VueTorrent/VueTorrent/releases/download/$vuetorrent/vuetorrent.zip"
echo "$vuetorrent_sha256  tmp/vuetorrent.zip" | shasum -a 256 -c - > /dev/null
unzip -q tmp/vuetorrent.zip -d tmp
# piecemeal and the seeder run as uid 65532, the *arrs via PUID.
chmod -R 0777 tmp

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    docker compose logs --no-color > tmp/compose.log 2>&1 || true
    echo "logs: test/e2e/tmp/compose.log"
  fi
  if [ -z "${KEEP:-}" ]; then
    docker compose down -v --remove-orphans > /dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT

docker compose up -d --build --wait --wait-timeout 300

E2E_PIECEMEAL_PASSWORD=$password go test -tags e2e -count=1 -v -timeout 15m ./
