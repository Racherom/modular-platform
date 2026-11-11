# syntax=docker/dockerfile:1
# The web image (spec 0030). MP_SOURCE picks where its source comes from:
#   local (default): the build context, the repository root:
#     docker build -f catalog/modules/web/Dockerfile .
#   git: the repository on GitHub at MP_SOURCE_REF, with any build context:
#     docker build --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<commit> -f Dockerfile .
# BuildKit builds only the stages the last one reaches, so a local build fetches nothing and a git
# build reads no context. A zip that names this image before its release builds it from this file.
# The editor imports the bundled sample from samples/, outside web/.

ARG MP_SOURCE=local
ARG MP_SOURCE_REF=main

FROM scratch AS source-local
COPY . /

FROM scratch AS source-git
ARG MP_SOURCE_REF
ADD https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF} /

FROM source-${MP_SOURCE} AS source

FROM node:26.13.0-alpine AS build
RUN npm install --global pnpm@12.16.1
WORKDIR /repo/web
COPY --from=source /web/package.json /web/pnpm-lock.yaml /web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY --from=source /samples/ /repo/samples/
COPY --from=source /contracts/ /repo/contracts/
COPY --from=source /web/ ./
RUN pnpm run build

# One image for every placement and mode: MP_BASE_PATH, MP_API_ROOT, MP_MODE and MP_SIGNED_OUT
# are filled into index.html at serve time.
FROM caddy:2.11.7-alpine
ARG MP_SOURCE_REF
LABEL org.opencontainers.image.revision=${MP_SOURCE_REF}
ENV MP_BASE_PATH="" \
    MP_API_ROOT="/renderer" \
    MP_MODE="online" \
    MP_SIGNED_OUT="index" \
    XDG_DATA_HOME=/tmp/caddy/data \
    XDG_CONFIG_HOME=/tmp/caddy/config
COPY --from=source /web/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /repo/web/dist /srv
USER 10001:10001
EXPOSE 8080
