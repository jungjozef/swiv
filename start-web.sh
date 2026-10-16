#!/bin/sh
# Builds the browser version, serves web/ with python3 and opens it in the
# browser. SWIV.adf is copied into web/ from the project directory if needed.
# Usage: ./start-web.sh [port]      (default 8080; Ctrl-C stops the server)
set -e
cd "$(dirname "$0")"
PORT="${1:-8080}"

if [ ! -f web/SWIV.adf ]; then
	if [ -f SWIV.adf ]; then
		cp SWIV.adf web/
	else
		echo "SWIV.adf not found: put it in $(pwd) or in $(pwd)/web" >&2
		exit 1
	fi
fi

tools/build_wasm.sh

URL="http://localhost:$PORT/"
python3 -m http.server -d web -b 127.0.0.1 "$PORT" &
SERVER=$!
trap 'kill $SERVER 2>/dev/null' EXIT INT TERM
sleep 1

if command -v open >/dev/null 2>&1; then
	open "$URL"
elif command -v xdg-open >/dev/null 2>&1; then
	xdg-open "$URL"
else
	echo "open $URL in your browser"
fi
echo "serving $URL (Ctrl-C to stop)"
wait $SERVER
