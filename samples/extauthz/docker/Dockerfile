ARG BASE_VERSION=latest

FROM gcr.io/istio-release/base:${BASE_VERSION}

ARG TARGETARCH
COPY ${TARGETARCH:-amd64}/extauthz /usr/local/bin/extauthz

ENTRYPOINT ["/usr/local/bin/extauthz"]
