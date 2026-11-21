# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# The job image of the oidc-provisioner capability (spec 0037, spec 0030 AC-14): busybox sh and
# sleep, curl and jq, nothing else. It runs the provision and revoke scripts of
# catalog/capabilities/oidc-provisioner.yaml. It holds no repository code, so there is no
# MP_SOURCE: the build context is this folder alone.
#   docker build --build-arg MP_SOURCE_REF=<ref> -f Dockerfile .
# The apk versions are those of the Alpine release the base digest holds; bump them by hand with
# the digest (Renovate refreshes the digest only).
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
ARG MP_SOURCE_REF=main
LABEL org.opencontainers.image.revision=${MP_SOURCE_REF}
RUN apk add --no-cache curl=8.23.0-r0 jq=1.8.2-r0
USER 65534:65534
CMD ["sleep", "infinity"]
