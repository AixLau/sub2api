#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: deploy/remote-compose-deploy.sh --remote SSH_TARGET [options]

Build the current Git commit as a linux/amd64 image locally, upload it, and
replace only the Sub2API service in an existing remote Compose deployment.

Options:
  --remote TARGET       SSH target (required)
  --repo PATH           Repository root (default: parent of this script)
  --compose-dir PATH    Remote Compose directory (default: /opt/sub2api/deploy)
  --compose-file FILE   Remote Compose file (default: docker-compose.local.yml)
  --service NAME        Compose service/container (default: sub2api)
  --image-repo NAME     Image repository (default: sub2api)
  --health-url URL      Remote health URL (default: http://127.0.0.1:8080/health)
  --remote-tar-dir PATH Remote archive directory (default: /tmp)
  --allow-dirty         Allow tracked worktree changes
  --skip-build          Deploy the expected image already present locally
  -h, --help            Show this help
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${script_dir}/.." && pwd)"
remote=""
compose_dir="/opt/sub2api/deploy"
compose_file="docker-compose.local.yml"
service="sub2api"
image_repo="sub2api"
health_url="http://127.0.0.1:8080/health"
remote_tar_dir="/tmp"
allow_dirty="false"
skip_build="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --remote) remote="$2"; shift 2 ;;
    --repo) repo="$2"; shift 2 ;;
    --compose-dir) compose_dir="$2"; shift 2 ;;
    --compose-file) compose_file="$2"; shift 2 ;;
    --service) service="$2"; shift 2 ;;
    --image-repo) image_repo="$2"; shift 2 ;;
    --health-url) health_url="$2"; shift 2 ;;
    --remote-tar-dir) remote_tar_dir="$2"; shift 2 ;;
    --allow-dirty) allow_dirty="true"; shift ;;
    --skip-build) skip_build="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if [[ -z "$remote" ]]; then
  echo "--remote is required" >&2
  usage >&2
  exit 1
fi

if [[ ! "$service" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "invalid service name: $service" >&2
  exit 1
fi

if [[ ! "$image_repo" =~ ^[A-Za-z0-9._/-]+$ ]]; then
  echo "invalid image repository: $image_repo" >&2
  exit 1
fi

if [[ ! "$compose_dir" =~ ^/[A-Za-z0-9._/-]+$ ]] ||
   [[ ! "$remote_tar_dir" =~ ^/[A-Za-z0-9._/-]+$ ]] ||
   [[ ! "$compose_file" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "compose and archive paths must be simple absolute/relative paths" >&2
  exit 1
fi

for command in git docker gzip rsync shasum ssh; do
  require_cmd "$command"
done

repo="$(cd "$repo" && pwd)"
cd "$repo"

if [[ "$allow_dirty" != "true" ]]; then
  dirty="$(git status --short --untracked-files=no)"
  if [[ -n "$dirty" ]]; then
    echo "tracked worktree changes exist; commit/stash them or pass --allow-dirty" >&2
    echo "$dirty" >&2
    exit 1
  fi
fi

commit="$(git rev-parse --short HEAD)"
version="$(tr -d '\r\n' < backend/cmd/server/VERSION)"
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
image="${image_repo}:${commit}-amd64"
out="dist/deploy-${commit}"
archive="${out}/sub2api-image-${commit}-amd64.tar.gz"
remote_archive="${remote_tar_dir%/}/$(basename "$archive")"

mkdir -p "$out"

echo "[local] commit=$commit version=$version image=$image"
ssh "$remote" 'bash -s' -- "$compose_dir" "$compose_file" <<'REMOTE_PREFLIGHT'
set -euo pipefail
compose_dir="$1"
compose_file="$2"
for command in docker gzip rsync sha256sum; do
  command -v "$command" >/dev/null || {
    echo "remote is missing required command: $command" >&2
    exit 1
  }
done
docker compose version >/dev/null
test -f "${compose_dir%/}/${compose_file}"
REMOTE_PREFLIGHT

if [[ "$skip_build" != "true" ]]; then
  docker buildx build \
    --platform linux/amd64 \
    --build-arg "VERSION=${version}" \
    --build-arg "COMMIT=${commit}" \
    --build-arg "DATE=${build_date}" \
    --load \
    -t "$image" \
    -f Dockerfile .
else
  docker image inspect "$image" >/dev/null
fi

docker image inspect "$image" --format '[local] image={{.Id}} arch={{.Architecture}} os={{.Os}}'
echo "[local] exporting compressed image archive"
docker save "$image" | gzip -1 > "$archive"
local_sum="$(shasum -a 256 "$archive" | cut -d ' ' -f1)"
echo "[local] archive=$archive sha256=$local_sum"

echo "[remote] uploading with resume support to ${remote}:${remote_archive}"
rsync --checksum --partial --inplace --progress \
  -e 'ssh -o ServerAliveInterval=15 -o ServerAliveCountMax=6' \
  "$archive" "${remote}:${remote_archive}"

remote_sum="$(ssh "$remote" "sha256sum '$remote_archive'" | cut -d ' ' -f1)"
if [[ "$local_sum" != "$remote_sum" ]]; then
  echo "archive checksum mismatch: local=$local_sum remote=$remote_sum" >&2
  exit 1
fi

echo "[remote] loading image and replacing only $service"
ssh "$remote" 'bash -s' -- \
  "$remote_archive" "$compose_dir" "$compose_file" "$service" "$image" "$image_repo" "$health_url" <<'REMOTE'
set -euo pipefail

archive="$1"
compose_dir="$2"
compose_file="$3"
service="$4"
image="$5"
image_repo="$6"
health_url="$7"

gzip -dc "$archive" | docker load
cd "$compose_dir"

mapfile -t image_lines < <(awk -v service="$service" '
  $0 ~ "^  " service ":[[:space:]]*$" { in_service = 1; next }
  in_service && $0 ~ "^  [A-Za-z0-9_-]+:[[:space:]]*$" { in_service = 0 }
  in_service && $0 ~ "^[[:space:]]+image:[[:space:]]*" { print NR }
' "$compose_file")
if [[ "${#image_lines[@]}" != "1" ]]; then
  echo "expected exactly one image entry for service '$service' in $compose_file; found ${#image_lines[@]}" >&2
  exit 1
fi

sed -i "${image_lines[0]}s|^[[:space:]]*image:.*|    image: ${image}|" "$compose_file"
grep -n "image: ${image}" "$compose_file"
docker compose -f "$compose_file" up -d --no-deps "$service"

for _ in $(seq 1 30); do
  status="$(docker inspect "$service" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}')"
  echo "health=$status"
  [[ "$status" == "healthy" ]] && break
  sleep 2
done

status="$(docker inspect "$service" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}')"
if [[ "$status" != "healthy" ]]; then
  docker logs --tail=120 "$service"
  exit 1
fi

docker inspect "$service" --format 'container_image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{end}}'
curl -fsS "$health_url"
REMOTE

echo
echo "[done] deployed $image to $remote"
