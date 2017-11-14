FROM ubuntu:xenial
ADD envoy /usr/local/bin/envoy
ADD server /
ADD bootstrap.json /
ADD envoy.jsonnet /

ENTRYPOINT ["/server"]
