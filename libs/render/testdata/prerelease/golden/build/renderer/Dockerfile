# syntax=docker/dockerfile:1
# The renderer image (spec 0030). MP_SOURCE picks where its source comes from:
#   local (default): the build context, the repository root:
#     docker build -f catalog/modules/renderer/Dockerfile .
#   git: the repository on GitHub at MP_SOURCE_REF, with any build context:
#     docker build --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<commit> -f Dockerfile .
# BuildKit builds only the stages the last one reaches, so a local build fetches nothing and a git
# build reads no context. A zip that names this image before its release builds it from this file.
ARG MP_SOURCE=local
ARG MP_SOURCE_REF=main

FROM scratch AS source-local
COPY . /

FROM scratch AS source-git
ARG MP_SOURCE_REF
ADD https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF} /

FROM source-${MP_SOURCE} AS source

FROM golang:1.27.3-alpine AS build
WORKDIR /repo
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY --from=source /go.work /go.work.sum* ./
COPY --from=source /catalog/ catalog/
COPY --from=source /libs/ libs/
COPY --from=source /sdk/ sdk/
# go.work lists every service module, so all of them must be present
COPY --from=source /services/ services/
# declared here, not above, so a new commit keeps the cached layers before this step
ARG MP_SOURCE_REF
# the renderer writes its source ref into every zip that builds a platform image (AC-10)
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w -X main.sourceRef=${MP_SOURCE_REF}" -o /out/renderer ./services/renderer/cmd/renderer

FROM gcr.io/distroless/static-debian12:nonroot
ARG MP_SOURCE_REF
LABEL org.opencontainers.image.revision=${MP_SOURCE_REF}
COPY --from=build /out/renderer /renderer
USER nonroot:nonroot
EXPOSE 8080 8443
ENTRYPOINT ["/renderer"]
