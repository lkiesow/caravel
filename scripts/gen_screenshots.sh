#!/usr/bin/env bash
# Regenerates the documentation screenshots in docs/assets/screenshots/.
#
#   scripts/gen_screenshots.sh            # or: make screenshots
#   PHOTO_DIR=~/my-photos scripts/gen_screenshots.sh
#
# Output is committed, this script is not part of any build -- the same contract
# as scripts/gen_icons.py and scripts/gen_icon_sprite.py. Re-run it when the UI
# moves, and diff the result.
#
# It runs against a **throwaway instance of its own**: its own SQLite database
# and upload directory in a temp dir, on its own port. That is not tidiness. The
# UI suite drives the shared seeded scenarios from `make dev-reset`, and a script
# that wrote to those would poison them for the next `make test-ui` run -- a
# hazard todo.md records twice. Nothing here touches the dev database.
set -euo pipefail

cd "$(dirname "$0")/.."

PORT="${PORT:-8099}"
PHOTO_DIR="${PHOTO_DIR:-images}"
OUT_DIR="docs/assets/screenshots"

work="$(mktemp -d)"
server_pid=""

cleanup() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

if ss -ltn 2>/dev/null | grep -q ":${PORT} "; then
  echo "screenshots: port ${PORT} is already in use — set PORT=… to pick another" >&2
  exit 1
fi

# Photos are optional, but their absence changes the output, so say so loudly
# rather than quietly producing a different set of images than the committed
# ones. They are deliberately not in the repository: they are the author's own
# and are used to dress the set, never published as files.
if [[ -d "$PHOTO_DIR" ]] && compgen -G "$PHOTO_DIR/*" > /dev/null; then
  echo "screenshots: dressing the set with photos from $PHOTO_DIR"
else
  cat >&2 <<WARN
screenshots: no photos found in '$PHOTO_DIR'.
  The run will continue, but every image in the UI will be the seeder's own
  343x200 test-sheet crop rather than a photograph, so the output will NOT match
  the committed screenshots. Point PHOTO_DIR at a directory of jpegs to match.
WARN
fi

export CARAVEL_PORT="$PORT"
export CARAVEL_DB_DSN="$work/screenshots.db"
export CARAVEL_UPLOAD_DIR="$work/uploads"
# Served from disk so a CSS change is picked up without rebuilding, matching
# `make dev`. The screenshots should show the working tree, not the last build.
export CARAVEL_WEB_DIR=web
# No assistant: a real key would put a live model in the loop of a screenshot
# run, and the stub is what the UI suite uses to render the panel.
export CARAVEL_LLM_URL=stub
export CARAVEL_LLM_MODEL=stub
export CARAVEL_SEARCH_PROVIDER=stub

echo "screenshots: building"
go build -o "$work/caravel" ./cmd/caravel

echo "screenshots: starting an isolated instance on :$PORT"
"$work/caravel" > "$work/server.log" 2>&1 &
server_pid=$!

for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/api/health" > /dev/null 2>&1; then break; fi
  if ! kill -0 "$server_pid" 2>/dev/null; then
    echo "screenshots: the server exited during startup:" >&2
    cat "$work/server.log" >&2
    exit 1
  fi
  sleep 0.2
done

# Only the `full` scenario. The others exist to give the UI suite its awkward
# shapes (no dates, one pin, days outside the range) and would fill the trips
# list with seven "Demo:" cards, which is not what the app looks like in use.
echo "screenshots: seeding"
go run ./cmd/seed -scenario full >> "$work/server.log" 2>&1

echo "screenshots: capturing"
mkdir -p "$OUT_DIR"
CARAVEL_TEST_URL="http://127.0.0.1:$PORT" \
  SCREENSHOT_OUT="$OUT_DIR" \
  SCREENSHOT_PHOTOS="$PHOTO_DIR" \
  node scripts/gen_screenshots.mjs

# Quantised because these are committed, and committed twice over: every
# regeneration adds the whole set to the history again. At device scale 2 the raw
# captures came to 3.3M; this brings the set under 1M with no visible loss on
# text or map tiles, which is the trade worth making for a screenshot.
if command -v pngquant > /dev/null; then
  before="$(du -sk "$OUT_DIR" | cut -f1)"
  for f in "$OUT_DIR"/*.png; do
    if pngquant --quality 70-92 --speed 1 --strip --force --output "$f.opt" "$f" 2>/dev/null; then
      mv "$f.opt" "$f"
    else
      # pngquant refuses when it cannot hit the quality floor. Keeping the
      # original is correct; losing the file is not.
      rm -f "$f.opt"
    fi
  done
  echo "screenshots: optimised ${before}K -> $(du -sk "$OUT_DIR" | cut -f1)K"
else
  echo "screenshots: pngquant not found — output is ~3x larger than the committed set" >&2
fi

echo "screenshots: done — $(ls -1 "$OUT_DIR" | wc -l) file(s) in $OUT_DIR"
