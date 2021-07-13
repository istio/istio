ARG BASE_VERSION=latest

FROM gcr.io/istio-release/base:${BASE_VERSION}

ARG TARGETARCH
COPY ${TARGETARCH:-amd64}/client /usr/local/bin/client
COPY ${TARGETARCH:-amd64}/server /usr/local/bin/server
COPY certs/cert.crt /cert.crt
COPY certs/cert.key /cert.key

ENTRYPOINT ["/usr/local/bin/server"]
