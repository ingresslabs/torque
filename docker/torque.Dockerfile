# syntax=docker/dockerfile:1

# Minimal runtime image that contains only the torque binary.
#
# The CI workflow builds `bin/torque-linux-{amd64,arm64}` into this build context
# and then `docker buildx` selects the correct one via TARGETARCH.

ARG TARGETARCH

FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETARCH
COPY bin/torque-linux-${TARGETARCH} /usr/local/bin/torque

ENTRYPOINT ["/usr/local/bin/torque"]
