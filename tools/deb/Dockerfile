# Base dockerfile containing ubuntu and istio debian.
# Can be used for testing
FROM ubuntu:xenial

ADD istio.deb /tmp
RUN apt-get update && apt-get install -y \
    iptables \
    && rm -rf /var/lib/apt/lists/*

RUN dpkg -i /tmp/istio.deb && rm /tmp/istio.deb
