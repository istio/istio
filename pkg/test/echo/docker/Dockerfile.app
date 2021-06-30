ARG BASE_VERSION=latest

FROM gcr.io/istio-release/base:${BASE_VERSION}

ARG TARGETARCH
COPY client-${TARGETARCH:-amd64} /usr/local/bin/client
COPY server-${TARGETARCH:-amd64} /usr/local/bin/server
COPY certs/cert.crt /cert.crt
COPY certs/cert.key /cert.key

ENTRYPOINT ["/usr/local/bin/server"]
