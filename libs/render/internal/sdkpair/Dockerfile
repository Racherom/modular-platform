# syntax=docker/dockerfile:1
# The two modules of the render's end to end test, one target each. Build from the repository
# root so go.work applies:
#   docker build -f libs/render/internal/sdkpair/Dockerfile --target pair-provider -t modular-platform/pair-provider:test .
#   docker build -f libs/render/internal/sdkpair/Dockerfile --target pair-consumer -t modular-platform/pair-consumer:test .

FROM golang:1.27.3-alpine AS build
WORKDIR /repo
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.work go.work.sum* ./
COPY catalog/ catalog/
COPY libs/ libs/
COPY sdk/ sdk/
COPY services/planner/ services/planner/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/ ./libs/render/internal/sdkpair/cmd/...

FROM gcr.io/distroless/static-debian12:nonroot AS pair-provider
COPY --from=build /out/pair-provider /pair-provider
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/pair-provider"]

FROM gcr.io/distroless/static-debian12:nonroot AS pair-consumer
COPY --from=build /out/pair-consumer /pair-consumer
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/pair-consumer"]
