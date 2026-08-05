#!/usr/bin/env bash

set -Eeuo pipefail

usage() {
  cat <<'EOF'
Usage: blue-green-deploy.sh --image IMAGE [options]

Start the inactive Sub2API color against the existing PostgreSQL and Redis,
wait until it is ready, switch every Caddy Sub2API upstream atomically, and
stop the previous color after connection draining.

Options:
  --image IMAGE          Required image tag already loaded on this host.
  --deploy-dir DIR       Compose directory (default: /opt/sub2api/deploy).
  --caddyfile FILE       Caddyfile to switch (default: /etc/caddy/Caddyfile).
  --ready-timeout SEC    Candidate readiness timeout (default: 90).
  --drain-timeout SEC    Maximum old-instance drain time (default: 180).
  --keep-previous        Keep the previous instance running after cutover.
  --force-stop-after-drain
                         Stop the previous instance at the drain timeout even
                         when connections remain (may interrupt streams).
  -h, --help             Show this help.
EOF
}

image=""
deploy_dir="/opt/sub2api/deploy"
caddyfile="/etc/caddy/Caddyfile"
ready_timeout=90
drain_timeout=180
keep_previous=false
force_stop_after_drain=false
caddy_replacement_installed=false

restore_caddy_on_failure() {
  local exit_code="${1:-1}"
  trap - ERR INT TERM
  if [[ "$caddy_replacement_installed" == true && -f "${caddy_previous:-}" ]]; then
    install -m 0644 "$caddy_previous" "$caddyfile" || true
    systemctl reload caddy || true
    echo "deployment interrupted; previous Caddy upstream restored" >&2
  fi
  exit "$exit_code"
}

trap 'restore_caddy_on_failure $?' ERR
trap 'restore_caddy_on_failure 130' INT
trap 'restore_caddy_on_failure 143' TERM

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      image="${2:-}"
      shift 2
      ;;
    --deploy-dir)
      deploy_dir="${2:-}"
      shift 2
      ;;
    --caddyfile)
      caddyfile="${2:-}"
      shift 2
      ;;
    --ready-timeout)
      ready_timeout="${2:-}"
      shift 2
      ;;
    --drain-timeout)
      drain_timeout="${2:-}"
      shift 2
      ;;
    --keep-previous)
      keep_previous=true
      shift
      ;;
    --force-stop-after-drain)
      force_stop_after_drain=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$image" ]]; then
  echo "--image is required" >&2
  exit 2
fi
if ! [[ "$ready_timeout" =~ ^[0-9]+$ && "$drain_timeout" =~ ^[0-9]+$ ]]; then
  echo "timeouts must be non-negative integers" >&2
  exit 2
fi

compose_file="$deploy_dir/docker-compose.local.yml"
overlay_file="$deploy_dir/docker-compose.blue-green.yml"
env_file="$deploy_dir/.env"

for required_file in "$compose_file" "$overlay_file" "$env_file" "$caddyfile"; do
  if [[ ! -f "$required_file" ]]; then
    echo "required file not found: $required_file" >&2
    exit 1
  fi
done
if ! docker image inspect "$image" >/dev/null 2>&1; then
  echo "image is not loaded: $image" >&2
  exit 1
fi

mapfile -t active_ports < <(
  sed -nE 's/.*reverse_proxy[[:space:]]+127\.0\.0\.1:(8080|8081|8082).*/\1/p' "$caddyfile" | sort -u
)
if [[ ${#active_ports[@]} -ne 1 ]]; then
  echo "expected exactly one active Sub2API Caddy port, found: ${active_ports[*]:-none}" >&2
  exit 1
fi

active_port="${active_ports[0]}"
case "$active_port" in
  8080)
    active_service="sub2api"
    target_color="blue"
    target_service="sub2api-blue"
    target_port=8081
    ;;
  8081)
    active_service="sub2api-blue"
    target_color="green"
    target_service="sub2api-green"
    target_port=8082
    ;;
  8082)
    active_service="sub2api-green"
    target_color="blue"
    target_service="sub2api-blue"
    target_port=8081
    ;;
  *)
    echo "unsupported active port: $active_port" >&2
    exit 1
    ;;
esac

if [[ "$target_color" == "blue" ]]; then
  export SUB2API_BLUE_IMAGE="$image"
else
  export SUB2API_GREEN_IMAGE="$image"
fi

compose=(
  docker compose
  --env-file "$env_file"
  -f "$compose_file"
  -f "$overlay_file"
  --profile blue-green
)

echo "active=$active_service:$active_port target=$target_service:$target_port image=$image"
"${compose[@]}" config --quiet
"${compose[@]}" up -d --no-deps --force-recreate "$target_service"

ready=false
for ((second = 0; second < ready_timeout; second++)); do
  if curl --fail --silent --show-error --max-time 3 "http://127.0.0.1:${target_port}/health" >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  docker logs --tail=120 "$target_service" >&2 || true
  echo "candidate did not become ready within ${ready_timeout}s; traffic was not switched" >&2
  exit 1
fi

caddy_previous="${caddyfile}.sub2api-previous"
caddy_candidate="${caddyfile}.sub2api-candidate"
cp "$caddyfile" "$caddy_previous"
sed "s|reverse_proxy 127\.0\.0\.1:${active_port}|reverse_proxy 127.0.0.1:${target_port}|g" \
  "$caddyfile" > "$caddy_candidate"

old_count=$(grep -c "reverse_proxy 127.0.0.1:${active_port}" "$caddyfile" || true)
new_count=$(grep -c "reverse_proxy 127.0.0.1:${target_port}" "$caddy_candidate" || true)
if [[ "$old_count" -lt 1 || "$new_count" -ne "$old_count" ]]; then
  echo "Caddy upstream replacement count mismatch: old=$old_count new=$new_count" >&2
  exit 1
fi

caddy validate --config "$caddy_candidate" --adapter caddyfile >/dev/null
install -m 0644 "$caddy_candidate" "$caddyfile"
caddy_replacement_installed=true
if ! systemctl reload caddy; then
  install -m 0644 "$caddy_previous" "$caddyfile"
  systemctl reload caddy
  caddy_replacement_installed=false
  echo "Caddy reload failed; previous upstream restored" >&2
  exit 1
fi

if ! curl --fail --silent --show-error --max-time 5 "http://127.0.0.1:${target_port}/health" >/dev/null; then
  install -m 0644 "$caddy_previous" "$caddyfile"
  systemctl reload caddy
  caddy_replacement_installed=false
  echo "candidate failed immediately after cutover; previous upstream restored" >&2
  exit 1
fi
caddy_replacement_installed=false
printf 'service=%s\nport=%s\nimage=%s\n' "$target_service" "$target_port" "$image" \
  > "$deploy_dir/.sub2api-blue-green-active"

echo "traffic switched to $target_service on port $target_port"

if [[ "$keep_previous" == true ]]; then
  echo "previous instance retained: $active_service"
  exit 0
fi

remaining_connections=0
if [[ "$drain_timeout" -gt 0 ]]; then
  for ((second = 0; second < drain_timeout; second++)); do
    remaining_connections=$(ss -Htn state established "sport = :${active_port}" 2>/dev/null | wc -l | tr -d ' ')
    if [[ "$remaining_connections" == "0" ]]; then
      break
    fi
    sleep 1
  done
fi

if [[ "$remaining_connections" != "0" && "$force_stop_after_drain" != true ]]; then
  echo "previous instance retained with ${remaining_connections} established connection(s): $active_service"
  docker inspect "$target_service" --format 'active={{.Name}} image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'
  exit 0
fi

if docker inspect "$active_service" >/dev/null 2>&1; then
  docker stop --time 30 "$active_service" >/dev/null
  echo "previous instance stopped after drain: $active_service"
fi

docker inspect "$target_service" --format 'active={{.Name}} image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'
