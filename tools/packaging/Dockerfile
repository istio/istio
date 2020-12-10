# Base dockerfile containing ubuntu and istio debian.
# Can be used for testing

# The following section is used as base image if BASE_DISTRIBUTION=default
# hadolint ignore=DL3006
FROM ubuntu:bionic

# Root and istio are not intercepted
RUN adduser istio-test --system

# The recommendation to pin package version is a security risk, prevents getting security updates
# (or require impossible maintenance - knowing when any package had a fix and updating in countless places)
# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends iproute2 openssl iptables curl less net-tools \
    && apt-get clean \
    && rm -rf  /var/log/*log /var/lib/apt/lists/* /var/log/apt/* /var/lib/dpkg/*-old /var/cache/debconf/*-old

COPY *.yaml /var/lib/istio/config/
COPY certs/vm/* /var/run/secrets/istio/

COPY deb_test.sh /usr/local/bin/
COPY certs/cacerts/ /etc/cacerts/
COPY certs/vm/ /etc/certs/

COPY istio.deb /tmp
COPY istio-sidecar.deb /tmp

# Verify the debian files can be installed
RUN dpkg -i /tmp/istio-sidecar.deb && rm /tmp/istio-sidecar.deb
RUN dpkg -i /tmp/istio.deb && rm /tmp/istio.deb

