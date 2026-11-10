# syntax=docker/dockerfile:1
# Build from the repository root so go.work applies: docker build -f services/renderer/Dockerfile .

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
    go build -ldflags="-s -w" -o /out/renderer ./services/renderer/cmd/renderer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/renderer /renderer
USER nonroot:nonroot
EXPOSE 8080 8443
ENTRYPOINT ["/renderer"]
