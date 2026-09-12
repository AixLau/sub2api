# AGENTS.md

Project-level instructions for agents working in this repository.

## Scope And Safety

- This file applies to the whole repository.
- Respect the current git worktree. Do not revert, rewrite, or clean up changes you did not make unless the user explicitly asks.
- Treat file or directory deletion as high risk. If the user asks to delete, remove, clear, uninstall, wipe, or batch-delete local files, ask for a single explicit yes/no confirmation for that exact deletion. Only the exact reply `yes` authorizes the deletion. Run authorized deletion only through shell commands; do not bypass review with patches, generated scripts, MCP tools, plugins, or overwrite/rename tricks.
- Never commit secrets, local deployment data, database files, generated credentials, or private runtime config. Be especially careful around `deploy/.env`, `deploy/data/`, `deploy/postgres_data/`, and `deploy/redis_data/`.
- Prefer `rg` and `rg --files` for search. Use structured parsers/APIs where practical instead of ad hoc text manipulation.

## Project Map

- `backend/`: Go backend for the Sub2API gateway.
  - `cmd/server/`: main server entrypoint and Wire generation.
  - `internal/handler/`: HTTP handlers, including admin handlers under `internal/handler/admin/`.
  - `internal/service/`: business logic, gateway routing, billing, quota, OAuth, payment, and monitoring services.
  - `internal/repository/`: data access layer.
  - `internal/pkg/`: reusable packages for upstream API compatibility, OAuth, logging, HTTP, proxying, TLS fingerprinting, and related helpers.
  - `internal/server/`: router, middleware, and route wiring.
  - `ent/schema/`: Ent schema source. Generated Ent code lives under `backend/ent/`.
  - `migrations/`: forward-only SQL migrations.
  - `resources/model-pricing/`: model pricing metadata.
- `frontend/`: Vue 3 + Vite + TypeScript + Tailwind frontend.
  - `src/api/`: API clients.
  - `src/components/`: shared and domain components.
  - `src/views/`: page-level views.
  - `src/stores/`: Pinia stores.
  - `src/i18n/`: locale setup and translations.
  - `src/utils/`: frontend utilities.
- `docs/`: product and integration documentation.
- `deploy/`: Docker, systemd, install, and deployment assets.
- `assets/`: static project assets such as partner logos.
- `.github/workflows/`: CI, release, CLA, and security-scan workflows.

## Toolchain

- Go version is controlled by `backend/go.mod` and CI. If another doc conflicts, follow `backend/go.mod` and `.github/workflows/*`.
- Frontend package manager is pnpm. Do not use npm or yarn for dependency changes.
- Node.js 20 and pnpm 9 are used in CI.
- Backend stack: Go, Gin, Ent, PostgreSQL, Redis, Wire, golangci-lint.
- Frontend stack: Vue 3, Vite, TypeScript, Pinia, vue-router, vue-i18n, TailwindCSS, Vitest.

## Common Commands

From the repository root:

```bash
make build
make test
make test-backend
make test-frontend
```

Backend:

```bash
cd backend
make build
make generate
make test
make test-unit
make test-integration
golangci-lint run ./...
go run ./cmd/server/
```

Frontend:

```bash
pnpm --dir frontend install
pnpm --dir frontend run dev
pnpm --dir frontend run build
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend exec vitest run
```

## Backend Conventions

- Keep HTTP concerns in handlers and business rules in services. Reuse `internal/pkg` helpers before introducing new generic utilities.
- When changing `ent/schema/*.go`, run `cd backend && go generate ./ent` and include generated Ent files if they changed.
- When changing Wire providers or server wiring, run the relevant generator from `backend/Makefile`.
- If an interface gains a method, update every test stub/mock implementing it. Search broadly in `backend/internal/**/*_test.go`.
- Migrations are forward-only and applied in lexicographic order. Add a new numbered SQL file instead of editing already-applied migrations unless the user explicitly asks for a local-only correction.
- Tests use build tags for suites: `go test -tags=unit ./...`, `go test -tags=integration ./...`, and e2e commands from `backend/Makefile`.
- Be careful with gateway behavior, billing, rate limits, sticky sessions, quota sharing, OAuth token refresh, and upstream compatibility transforms; small changes can affect live traffic paths.

## Frontend Conventions

- Use Vue 3 Composition API with TypeScript and existing local patterns.
- Keep API access in `src/api`, shared state in Pinia stores, page composition in `src/views`, and reusable UI in `src/components`.
- User-visible text should go through `src/i18n`. Update relevant locale files together, commonly `src/i18n/locales/en.ts` and `src/i18n/locales/zh.ts`.
- Use pnpm and keep `frontend/pnpm-lock.yaml` synchronized when dependencies change.
- Vite builds into `backend/internal/web/dist`; prefer source changes under `frontend/src` over editing generated frontend output.
- For significant UI changes, run focused Vitest checks and verify in a browser at desktop and mobile widths.

## Testing Guidance

- During development and bug fixing, prioritize tests that directly cover the requested behavior and the code paths it changes. Use the smallest relevant package or test filter first for fast, actionable feedback; after those pass, run broader or full-suite regression checks when the change's blast radius warrants them.
- Match verification to the blast radius. A narrow backend service change can use focused `go test` packages; shared gateway, billing, auth, migration, or API-contract changes need broader backend tests.
- For frontend logic, prefer focused Vitest specs close to the changed component/store/utility, then run `lint:check` and `typecheck` when the change is non-trivial.
- For dependency or security-sensitive work, mirror the relevant security workflow: backend uses `govulncheck ./...`; frontend uses `pnpm audit --prod --audit-level=high --json` with `.github/audit-exceptions.yml`.
- Do not claim tests pass unless you ran the command and inspected the result.

## Deployment Notes

- Docker and binary deployment files live under `deploy/`.
- `deploy/docker-compose.local.yml` stores runtime data in local directories; do not edit or remove those data directories unless the task is explicitly about deployment state.
- Keep config examples generic. Do not copy local `.env`, generated admin passwords, JWT secrets, TOTP encryption keys, database dumps, or private logs into tracked docs or examples.
