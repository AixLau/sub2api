# Sub2API Docker Image

The published image contains the Sub2API backend, embedded web frontend, runtime resources, and PostgreSQL client tools used by the data-management features.

## Recommended Use

Use the maintained Compose files instead of assembling an application, PostgreSQL, and Redis stack from an outdated inline example:

- `docker-compose.local.yml`: complete stack with bind-mounted data directories
- `docker-compose.yml`: complete stack with named volumes
- `docker-compose.standalone.yml`: application only, using external PostgreSQL and Redis

See [README.md](./README.md) for setup, upgrade, rollback, backup, and health-check instructions.

## Standalone Container

When PostgreSQL and Redis already exist, the minimum shape is:

```bash
docker run -d \
  --name sub2api \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -e AUTO_SETUP=true \
  -e SERVER_HOST=0.0.0.0 \
  -e SERVER_PORT=8080 \
  -e DATABASE_HOST=postgres.example.internal \
  -e DATABASE_PASSWORD='<set-at-runtime>' \
  -e REDIS_HOST=redis.example.internal \
  -e JWT_SECRET='<set-at-runtime>' \
  -e TOTP_ENCRYPTION_KEY='<set-at-runtime>' \
  -v sub2api_data:/app/data \
  weishaw/sub2api:latest
```

Prefer `--env-file` over putting secrets in shell history. Never commit the env file.

## Health Check

The image defines a Docker health check against the container's `/health` endpoint. Verify both Docker state and HTTP response:

```bash
docker inspect sub2api --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}'
curl -fsS http://127.0.0.1:8080/health
```

Expected HTTP response:

```json
{"status":"ok"}
```

## Tags And Architectures

- `latest`: latest stable release
- `x.y.z`: exact release
- `x.y`: latest patch in a minor release
- `x`: latest minor in a major release
- Supported release architectures: `linux/amd64`, `linux/arm64`

For production, pin an exact version instead of relying on `latest` so rollback remains deterministic.

## Build Locally

```bash
commit=$(git rev-parse --short HEAD)
version=$(tr -d '\r\n' < backend/cmd/server/VERSION)
docker buildx build \
  --platform linux/amd64 \
  --build-arg "VERSION=${version}" \
  --build-arg "COMMIT=${commit}" \
  --load \
  -t "sub2api:${commit}-amd64" \
  -f Dockerfile .
```

Use `deploy/remote-compose-deploy.sh` to publish that commit to an existing Compose server without building on the server.

## Links

- [GitHub repository](https://github.com/Wei-Shaw/sub2api)
- [Deployment guide](./README.md)
