FROM scratch

WORKDIR /tmp/
COPY servicegraph /usr/local/bin/
COPY viz /tmp/js/viz/

EXPOSE 8088
ENTRYPOINT ["/usr/local/bin/servicegraph", "--assetDir=/tmp"]
