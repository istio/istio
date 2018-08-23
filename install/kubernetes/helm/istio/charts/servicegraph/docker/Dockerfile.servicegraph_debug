FROM ubuntu:xenial

WORKDIR /tmp/
COPY servicegraph /usr/local/bin/
COPY js /tmp/js/
COPY force /tmp/force/

EXPOSE 8088
CMD ["/usr/local/bin/servicegraph", "--assetDir=/tmp"]
