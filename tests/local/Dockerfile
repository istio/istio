FROM ubuntu:xenial
RUN apt-get update \
  && apt-get install -y iptables curl

ADD . /tmp

RUN dpkg -i /tmp/istio-proxy_amd64.deb
RUN dpkg -i /tmp/istio-node-agent_amd64.deb && rm /tmp/*.deb
RUN dpkg -i /tmp/istio-auth-node-agent_amd64.deb && rm /tmp/*.deb


