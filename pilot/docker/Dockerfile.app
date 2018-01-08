ARG ISTIO_BIN=.
FROM scratch
ADD ${ISTIO_BIN}/pilot-test-client /usr/local/bin/client
ADD ${ISTIO_BIN}/pilot-test-server /usr/local/bin/server
ADD certs/cert.crt /cert.crt
ADD certs/cert.key /cert.key
ENTRYPOINT ["/usr/local/bin/server"]
