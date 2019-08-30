# hadolint ignore=DL3006
FROM istionightly/base_debug

COPY pkg-test-echo-cmd-client /usr/local/bin/client
COPY pkg-test-echo-cmd-server /usr/local/bin/server
COPY certs/cert.crt /cert.crt
COPY certs/cert.key /cert.key
ENTRYPOINT ["/usr/local/bin/server"]
