FROM        quay.io/prometheus/busybox:latest
LABEL maintainer="The Prometheus Authors <prometheus-developers@googlegroups.com>"

COPY prom2json /bin/prom2json

USER nobody
ENTRYPOINT  [ "/bin/prom2json" ]
