FROM ubuntu:xenial
RUN apt-get update \
  && apt-get install -y iptables curl

ADD . /tmp

RUN dpkg -i /tmp/istio-proxy-envoy.deb
RUN dpkg -i /tmp/istio-agent.deb
RUN dpkg -i /tmp/istio-auth-node-agent.deb && rm /tmp/*.deb


