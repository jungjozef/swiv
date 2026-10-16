#!/bin/sh
# Builds the browser version into web/: swiv.wasm (GOOS=js GOARCH=wasm, which
# selects main_wasm.go) and the matching wasm_exec.js. Put your own SWIV.adf
# next to them and serve the directory, e.g.
#   cp SWIV.adf web/ && python3 -m http.server -d web 8080
set -e
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o web/swiv.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
echo "built web/swiv.wasm ($(wc -c < web/swiv.wasm) bytes)"
