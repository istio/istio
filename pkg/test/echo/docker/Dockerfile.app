ARG BASE_VERSION=latest
ARG ISTIO_BASE_REGISTRY=gcr.io/istio-release

FROM ${ISTIO_BASE_REGISTRY}/base:${BASE_VERSION}

ARG TARGETARCH
COPY ${TARGETARCH:-amd64}/client /usr/local/bin/client
COPY ${TARGETARCH:-amd64}/server /usr/local/bin/server
COPY dns/cert-chain.pem /cert.crt
COPY dns/key.pem /cert.key

# Add a user that will run the application. This allows running as this user and capture iptables
RUN useradd -m --uid 1338 application
USER 1338

ENTRYPOINT ["/usr/local/bin/server"]
