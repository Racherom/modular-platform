# syntax=docker/dockerfile:1
# Build from the repository root: docker build -f web/Dockerfile .
# The editor imports the bundled sample from samples/, outside web/.

FROM node:26.13.0-alpine AS build
RUN npm install --global pnpm@12.16.1
WORKDIR /repo/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY samples/ /repo/samples/
COPY contracts/ /repo/contracts/
COPY web/ ./
RUN pnpm run build

# One image for every placement and mode: MP_BASE_PATH, MP_API_ROOT, MP_MODE and MP_SIGNED_OUT
# are filled into index.html at serve time.
FROM caddy:2.11.7-alpine
ENV MP_BASE_PATH="" \
    MP_API_ROOT="/planner" \
    MP_MODE="online" \
    MP_SIGNED_OUT="index" \
    XDG_DATA_HOME=/tmp/caddy/data \
    XDG_CONFIG_HOME=/tmp/caddy/config
COPY web/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /repo/web/dist /srv
USER 10001:10001
EXPOSE 8080
