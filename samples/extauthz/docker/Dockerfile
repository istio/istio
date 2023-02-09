ARG BASE_VERSION=latest
ARG ISTIO_BASE_REGISTRY=gcr.io/istio-release

FROM ${ISTIO_BASE_REGISTRY}/base:${BASE_VERSION}

ARG TARGETARCH
COPY ${TARGETARCH:-amd64}/extauthz /usr/local/bin/extauthz

ENTRYPOINT ["/usr/local/bin/extauthz"]
