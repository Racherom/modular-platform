# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# The job image of the oidc-provisioner capability (spec 0037, task 5): busybox sh and sleep, curl
# and jq, nothing else. It runs the provision and revoke scripts of catalog/capabilities/oidc-provisioner.yaml.
FROM alpine:3.24
ARG MP_SOURCE_REF
LABEL org.opencontainers.image.revision=${MP_SOURCE_REF}
RUN apk add --no-cache curl jq
USER 65534:65534
CMD ["sleep", "infinity"]
