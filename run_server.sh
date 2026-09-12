#!/bin/sh

SERVER_DIR=$(cd "$(dirname "$0")" && pwd)

# 后端启动配置
export PORT=${PORT:-8080}
export DATABASE_URL=${DATABASE_URL:-"$SERVER_DIR/metrochat.db"}
export JWT_SECRET=${JWT_SECRET:-"local-dev-secret-change-me-please-32-chars"}
export JWT_ISSUER=${JWT_ISSUER:-"metrochat"}
export ACCESS_TOKEN_TTL=${ACCESS_TOKEN_TTL:-0}  # 0 = no expiry (legacy client compatible)
export REFRESH_TOKEN_TTL=${REFRESH_TOKEN_TTL:-2592000} # 30 days

BIN_PATH=""
if [ -n "${BIN_NAME:-}" ] && [ -x "$SERVER_DIR/$BIN_NAME" ]; then
  BIN_PATH="$SERVER_DIR/$BIN_NAME"
else
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
  esac
  if [ -x "$SERVER_DIR/ocserver_${OS}_${ARCH}" ]; then
    BIN_PATH="$SERVER_DIR/ocserver_${OS}_${ARCH}"
  fi
fi

if [ -z "$BIN_PATH" ]; then
  echo "Server binary not found. Build it with: pwsh ./build.ps1"
  exit 1
fi

echo "Starting server on port $PORT..."
"$BIN_PATH"
