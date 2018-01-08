ARG ISTIO_BIN=.
FROM scratch

WORKDIR /tmp/
ADD ${ISTIO_BIN}/servicegraph /usr/local/bin/
ADD viz /tmp/js/

EXPOSE 8088
ENTRYPOINT ["/usr/local/bin/servicegraph", "--assetDir=/tmp"]
