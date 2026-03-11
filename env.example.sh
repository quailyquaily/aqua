#!/usr/bin/env bash

# Frontend API origin for Vite dev.
# Leave unset to default to http(s)://<current-host>:8089.
export VITE_AQUA_API_BASE="http://127.0.0.1:8089"

# Recommended frontend dev port.
export AQUA_WEB_DEV_PORT="3000"

# Usage:
#   source ./env.example.sh
#   pnpm --dir web dev --port "${AQUA_WEB_DEV_PORT}"
#   aqua serve --web-console --web-console-addr 127.0.0.1:8089 --console-web-dst-dir ./web/dist
