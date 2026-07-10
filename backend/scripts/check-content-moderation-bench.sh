#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 BASE CANDIDATE" >&2
  exit 2
fi

case "$1" in
  /*) baseline="$1" ;;
  *) baseline="$PWD/$1" ;;
esac
case "$2" in
  /*) candidate="$2" ;;
  *) candidate="$PWD/$2" ;;
esac

backend_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$backend_dir"
go run ./cmd/moderation-bench-guard "$baseline" "$candidate"
