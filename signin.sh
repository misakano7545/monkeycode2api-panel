#!/usr/bin/env bash
# 批量「额度到账检查 + 每日签到」。用法：./signin.sh
set -euo pipefail
cd "$(dirname "$0")"
if [ -x ./mc2api-signin ]; then
  exec ./mc2api-signin "$@"
fi
exec go run ./cmd/signin "$@"
