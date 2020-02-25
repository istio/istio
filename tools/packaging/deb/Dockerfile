# Base dockerfile containing ubuntu and istio debian.
# Can be used for testing

# The following section is used as base image if BASE_DISTRIBUTION=default
# hadolint ignore=DL3006
FROM ubuntu:bionic

# Directory to store DNS certificate and key
RUN mkdir -p /etc/dnscerts

COPY *.yaml /var/lib/istio/config/
COPY certs/ /var/lib/istio/
COPY certs/default/* /etc/certs/

COPY istio.deb /tmp
COPY istio-sidecar.deb /tmp
COPY deb_test.sh /usr/local/bin/

# Root and istio are not intercepted
RUN adduser istio-test --system

RUN apt-get update && apt-get install -y iproute2 iptables curl less net-tools

# Verify the debian files can be installed
RUN dpkg -i /tmp/istio-sidecar.deb && rm /tmp/istio-sidecar.deb
RUN dpkg -i /tmp/istio.deb && rm /tmp/istio.deb

