# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# The deployer-docker image (spec 0030). MP_SOURCE picks where its source comes from:
#   local (default): the build context, the repository root:
#     docker build -f catalog/modules/deployer-docker/Dockerfile .
#   git: the repository on GitHub at MP_SOURCE_REF, with any build context:
#     docker build --build-arg MP_SOURCE=git --build-arg MP_SOURCE_REF=<commit> -f Dockerfile .
# BuildKit builds only the stages the last one reaches, so a local build fetches nothing and a git
# build reads no context. A zip that names this image before its release builds it from this file.
# The base is the docker CLI image, which carries the compose plugin; the deployer runs as root
# because the Docker socket it holds is root equivalent anyway (spec 0001).

ARG MP_SOURCE=local
ARG MP_SOURCE_REF=main

FROM scratch AS source-local
COPY . /

FROM scratch AS source-git
ARG MP_SOURCE_REF
ADD https://github.com/racherom/modular-platform.git#${MP_SOURCE_REF} /

FROM source-${MP_SOURCE} AS source

FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /repo
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY --from=source /go.work /go.work.sum* ./
COPY --from=source /catalog/ catalog/
COPY --from=source /libs/ libs/
COPY --from=source /sdk/ sdk/
# go.work lists every service module, so all of them must be present
COPY --from=source /services/ services/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/deployer-docker ./services/deployer-docker/cmd/deployer-docker

FROM docker:29.9.2-cli@sha256:5e6baf67325a79f64ed3bed6863954d5e05d0f3d4e57cbdb14809346814bbfdb
ARG MP_SOURCE_REF
LABEL org.opencontainers.image.revision=${MP_SOURCE_REF}
COPY --from=build /out/deployer-docker /deployer-docker
EXPOSE 8080 8443
# the image's own entrypoint treats arguments as docker subcommands; replace it
ENTRYPOINT ["/deployer-docker"]
