#!/bin/sh
# Builds the static version of the site into dist/: the page plus apps.json,
# with no Go server and no live status section. Vercel runs this on deploy.
set -e
cd "$(dirname "$0")/.."
rm -rf dist
cp -R web dist
cp apps.json dist/apps.json
# Mark the page as static so it skips looking for the Go server's API.
sed 's/<html lang="en">/<html lang="en" data-static>/' web/index.html > dist/index.html
grep -q 'data-static' dist/index.html || { echo "build-static: couldn't mark index.html"; exit 1; }
echo "Built dist/ ($(ls dist/img | wc -l | tr -d ' ') images)"
