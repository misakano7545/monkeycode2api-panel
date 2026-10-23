#!/usr/bin/env bash
# 粘贴式登录：校验 session 并落盘到 auths/。
# 用法：./login.sh [-region cn|intl] [-image <uuid>]
set -euo pipefail
cd "$(dirname "$0")"
if [ -x ./mc2api-login ]; then
  exec ./mc2api-login "$@"
fi
exec go run ./cmd/login "$@"
