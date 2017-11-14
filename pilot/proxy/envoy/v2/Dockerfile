FROM gcr.io/istio-testing/envoy-debug:f8b4de9a80f1d6b500b3148e0e3364daffbb9dfc
ADD envoy /usr/local/bin/envoy
ADD server /
ADD bootstrap.json /
ADD envoy.jsonnet /

ENTRYPOINT ["/server"]
