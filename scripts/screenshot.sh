#!/bin/sh
# Takes a screenshot of a live app for its portfolio card.
#
#   ./scripts/screenshot.sh <app-id> <url>
#   ./scripts/screenshot.sh cubelab https://speed-cube-trainer.vercel.app
#
# Saves web/img/<app-id>.jpg (1200px wide). Needs Google Chrome on macOS.
set -e
[ $# -eq 2 ] || { echo "usage: $0 <app-id> <url>"; exit 1; }

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
OUT="$(cd "$(dirname "$0")/.." && pwd)/web/img"
TMP="$(mktemp -d)"

# SwiftShader renders 3D (WebGL) pages that would otherwise come out blank.
"$CHROME" --headless=new --use-angle=swiftshader --enable-unsafe-swiftshader \
  --hide-scrollbars --mute-audio --window-size=1280,800 --virtual-time-budget=12000 \
  --screenshot="$TMP/shot.png" "$2" >/dev/null 2>&1

sips -s format jpeg -s formatOptions 82 --resampleWidth 1200 "$TMP/shot.png" --out "$OUT/$1.jpg" >/dev/null
rm -rf "$TMP"
echo "Saved web/img/$1.jpg. Open it to check it looks right (login screens and pop-ups show up too)."
