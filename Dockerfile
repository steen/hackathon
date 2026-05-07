# syntax=docker/dockerfile:1.7
#
# Multi-stage build for the chat-server single-binary image.
#
# Stage 1 (web): pnpm install + `pnpm --filter web build` produces the
# Vite SPA at /build/apps/web/dist/.
#
# Stage 2 (go): copies the SPA into apps/server/internal/web/dist/ so
# `//go:embed all:dist` (apps/server/internal/web/embed.go) picks it up,
# then static-links the binary with CGO_ENABLED=0. The sqlite driver is
# modernc.org/sqlite (pure Go) so no C toolchain is needed.
#
# Stage 3 (final): distroless static-debian12 :nonroot. Single binary at
# /chat-server, EXPOSE 8080, runs as USER nonroot (UID 65532).
#
# No HEALTHCHECK instruction is set: distroless :nonroot has no shell,
# wget, or curl, so there is no in-container probe path today. Liveness
# is handled by the reverse proxy (see docs/ops/runbook.md from #792); a
# self-contained `--health-probe` flag is filed as #796.
#
# Operator notes:
#   The SQLite database file is not part of the image. Mount a volume
#   and override CHAT_DB_PATH to a path inside it, e.g.:
#
#     docker run --rm \
#       -e CHAT_JWT_SECRET="$(openssl rand -hex 24)" \
#       -e CHAT_INVITE_CODE=... \
#       -e CHAT_DB_PATH=/data/chat.db \
#       -v "$PWD/data":/data \
#       -p 8080:8080 \
#       chat-server:dev

# ---------- Stage 1: web ----------
FROM node:20-alpine AS web

# Pin pnpm to the major used by CI (.github/workflows/ci.yml: pnpm/action-setup@v4 with version: 9).
RUN corepack enable && corepack prepare pnpm@9 --activate

WORKDIR /build

# Copy lockfile + workspace manifests first so the install layer caches
# until any package.json or the lockfile actually changes. The web build
# depends on workspace packages (apps/web/vite.config.ts aliases
# @hackathon/api-client and @hackathon/chat-ui to their src/index.ts),
# so all workspace package.json files must be present before install.
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/web/package.json ./apps/web/
COPY apps/cli/package.json ./apps/cli/
COPY apps/server/package.json ./apps/server/
COPY packages/api-client/package.json ./packages/api-client/
COPY packages/chat-ui/package.json ./packages/chat-ui/
COPY packages/go-client/package.json ./packages/go-client/
COPY tests/package.json ./tests/
COPY tests/e2e/package.json ./tests/e2e/

RUN pnpm install --frozen-lockfile

# Copy the sources needed to build the web bundle. The vite.config.ts
# aliases pull from packages/api-client/src and packages/chat-ui/src, so
# both must be present.
COPY apps/web ./apps/web
COPY packages/api-client ./packages/api-client
COPY packages/chat-ui ./packages/chat-ui
COPY tsconfig.json ./tsconfig.json

RUN pnpm --filter web build

# ---------- Stage 2: go ----------
FROM golang:1.25 AS go

WORKDIR /src

# Cache module downloads on go.mod / go.sum churn only.
COPY go.mod go.sum ./
RUN go mod download

# Copy the Go source tree. apps/server/internal/web/dist/ already
# contains a placeholder index.html; we overwrite it with the real Vite
# bundle from the web stage so //go:embed picks up the production assets.
COPY apps ./apps
COPY migrations ./migrations

COPY --from=web /build/apps/web/dist/ ./apps/server/internal/web/dist/

# CGO_ENABLED=0 produces a static binary suitable for distroless static.
# -trimpath strips local paths from the binary; -ldflags '-s -w' drops
# the symbol and DWARF tables to shrink the image.
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags '-s -w' -o /chat-server ./apps/server

# ---------- Stage 3: final ----------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=go /chat-server /chat-server

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/chat-server"]
