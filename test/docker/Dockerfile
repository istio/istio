# Image for building and running Kind
# Kind will be used in a CI/CD system
# Already present: kubectl, kubeadm, ubuntu, docker
#
# make docker.istio-builder

FROM  kindest/node:v1.14.0

# Environment variables used in the build.
ENV     GOROOT=/usr/local/go
ENV     PATH=/usr/local/go/bin:/bin:/usr/bin:${PATH}

# Need to have docker installed
#RUN curl -s -L https://get.docker.io/ | sh -x

RUN  apt-get update && apt-get -qqy install make git

ADD kind /usr/local/bin
ADD helm /usr/local/bin
ADD repo /usr/local/bin

RUN curl -Lo - https://dl.google.com/go/go1.11.5.linux-amd64.tar.gz | tar -C /usr/local -xzf -

# It appears go test in istio/istio requires gcc
RUN  apt-get -qqy install build-essential autoconf libtool autotools-dev

#ADD bin/istioctl /usr/local/bin
ADD istioctl /usr/local/bin
