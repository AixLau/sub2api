#!/usr/bin/env bash
set -euo pipefail

plugin_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
backend_dir="$(cd "${plugin_dir}/../.." && pwd)"
targets="${TARGETS:-linux-amd64}"
version="1.0.0"
build_dir="${plugin_dir}/.build"
dist_dir="${plugin_dir}/dist"

mkdir -p "${build_dir}" "${dist_dir}"

IFS=',' read -r -a target_list <<< "${targets}"
for target in "${target_list[@]}"; do
  target="${target//[[:space:]]/}"
  case "${target}" in
    linux-amd64) goos=linux; goarch=amd64; binary="openai-codex-ticket" ;;
    linux-arm64) goos=linux; goarch=arm64; binary="openai-codex-ticket" ;;
    darwin-amd64) goos=darwin; goarch=amd64; binary="openai-codex-ticket" ;;
    darwin-arm64) goos=darwin; goarch=arm64; binary="openai-codex-ticket" ;;
    windows-amd64) goos=windows; goarch=amd64; binary="openai-codex-ticket.exe" ;;
    *) echo "unsupported target: ${target}" >&2; exit 1 ;;
  esac
  mkdir -p "${build_dir}/${target}"
  (cd "${backend_dir}" && CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath -ldflags "-s -w" -o "${build_dir}/${target}/${binary}" ./plugins/openai-codex-ticket/cmd/openai-codex-ticket)
done

packager_args=(-plugin-dir "${plugin_dir}" -output "${dist_dir}/openai-codex-ticket-${version}.s2plugin" -targets "${targets}")
if [[ -n "${SIGNING_KEY:-}" || -n "${KEY_ID:-}" ]]; then
  if [[ -z "${SIGNING_KEY:-}" || -z "${KEY_ID:-}" ]]; then
    echo "SIGNING_KEY and KEY_ID must be provided together" >&2
    exit 1
  fi
  packager_args+=( -signing-key "${SIGNING_KEY}" -key-id "${KEY_ID}" )
fi
(cd "${backend_dir}" && go run ./plugins/openai-codex-ticket/tools/packager "${packager_args[@]}")
echo "created ${dist_dir}/openai-codex-ticket-${version}.s2plugin"
