# Image based on buildkite docker agent, with the tools used for Istio installer and tests.
# This will run in a dockerfile or k8s - 'make test' should work.

FROM buildkite/agent:3-ubuntu

# Environment variables used in the build.
ENV     GOROOT=/usr/local/go
ENV     PATH=/usr/local/go/bin:/bin:/usr/bin:${PATH}

RUN  apt-get update && apt-get -qqy install make git

ADD kind /usr/local/bin
ADD helm /usr/local/bin
ADD repo /usr/local/bin

RUN curl -Lo - https://dl.google.com/go/go1.11.5.linux-amd64.tar.gz | tar -C /usr/local -xzf -

# It appears go test in istio/istio requires gcc
RUN  apt-get -qqy install build-essential autoconf libtool autotools-dev
