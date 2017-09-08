FROM lyft/envoy:latest

RUN mkdir /opt/istio
RUN mkdir /etc/envoy/

ADD ./pilot-discovery /opt/istio

ENTRYPOINT /opt/istio/pilot-discovery discovery -v 2 --registries Consul --consulserverURL http://consul:8500 --kubeconfig /istio/config
