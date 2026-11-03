# syntax=docker/dockerfile:1
# Build from the repository root so go.work applies: docker build -f services/deployer-docker/Dockerfile .
# The base is the docker CLI image, which carries the compose plugin; the deployer runs as root
# because the Docker socket it holds is root equivalent anyway (spec 0001).

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
    go build -ldflags="-s -w" -o /out/deployer-docker ./services/deployer-docker/cmd/deployer-docker

FROM docker:29.9.2-cli
COPY --from=build /out/deployer-docker /deployer-docker
EXPOSE 8080 8443
# the image's own entrypoint treats arguments as docker subcommands; replace it
ENTRYPOINT ["/deployer-docker"]
