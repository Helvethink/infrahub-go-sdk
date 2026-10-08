FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.23 AS builder

ARG PROJECT_NAME=infrahubctl
ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ENV CGO_ENABLED=0

WORKDIR /src

# The Alpine base image fixes the repository version. Do not pin a package
# revision here because Alpine removes superseded revisions from its mirrors.
# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY infrahubctl /infrahubctl

USER 33092

ENTRYPOINT ["/infrahubctl"]
