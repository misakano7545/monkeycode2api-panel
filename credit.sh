#!/usr/bin/env bash
# 钱包 / 额度日报。用法：./credit.sh [-json]
set -euo pipefail
cd "$(dirname "$0")"
if [ -x ./mc2api-credit ]; then
  exec ./mc2api-credit "$@"
fi
exec go run ./cmd/credit "$@"
