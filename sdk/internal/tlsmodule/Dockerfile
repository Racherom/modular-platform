# syntax=docker/dockerfile:1
# The module of the sdk's TLS tests against pebble. Build from the repository root so go.work
# applies:
#   docker build -f sdk/internal/tlsmodule/Dockerfile -t modular-platform/sdk-tls-module:test .

FROM golang:1.27.3-alpine AS build
WORKDIR /repo
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.work go.work.sum* ./
COPY catalog/ catalog/
COPY libs/ libs/
COPY sdk/ sdk/
# go.work lists every service module, so all of them must be present
COPY services/ services/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/tls-module ./sdk/internal/tlsmodule/cmd/tls-module

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tls-module /tls-module
USER nonroot:nonroot
EXPOSE 8443
ENTRYPOINT ["/tls-module"]
