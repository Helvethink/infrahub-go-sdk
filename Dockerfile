FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG PROJECT_NAME=infrahubctl
ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ENV CGO_ENABLED=0

WORKDIR /src

RUN apk add --no-cache ca-certificates

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY infrahubctl /infrahubctl

USER 33092

ENTRYPOINT ["/infrahubctl"]