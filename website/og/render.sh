#!/bin/sh
# Renders og/home.html to public/og-home.png, the home page's Open Graph
# card, with headless Chrome: at twice the size, then scaled down so text
# and the screenshot stay sharp.
#
#   website/og/render.sh                    on macOS with Google Chrome
#   CHROME=chromium website/og/render.sh    elsewhere
set -eu
cd "$(dirname "$0")"
chrome=${CHROME:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
"$chrome" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
  --window-size=1200,630 --virtual-time-budget=8000 \
  --screenshot="$tmp/og.png" "file://$PWD/home.html" 2> /dev/null
if command -v sips > /dev/null; then
  sips -z 630 1200 "$tmp/og.png" --out ../public/og-home.png > /dev/null
else
  magick "$tmp/og.png" -resize 1200x630 ../public/og-home.png
fi
echo "wrote public/og-home.png"
