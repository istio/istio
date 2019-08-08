# ISTIO_VERSION is used to specify the version of the release
ARG ISTIO_VERSION=latest

# The following section is used as base image if BASE_DISTRIBUTION=default
FROM docker.io/sdake/base_debug:${ISTIO_VERSION} as default

COPY pkg-test-echo-cmd-client /usr/local/bin/client
COPY pkg-test-echo-cmd-server /usr/local/bin/server
COPY certs/cert.crt /cert.crt
COPY certs/cert.key /cert.key
ENTRYPOINT ["/usr/local/bin/server"]
