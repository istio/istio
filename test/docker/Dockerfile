# Image for building and running Kind
# Kind will be used in a CI/CD system
# Already present: kubectl, kubeadm, ubuntu, docker
#
# make docker.istio-builder

FROM  kindest/node:v1.14.0

# Environment variables used in the build.
ENV     GOPATH=/go
ENV     PATH=/go/bin:/go/out/linux_amd64:$PATH

# Need to have docker installed
#RUN curl -s -L https://get.docker.io/ | sh -x


RUN  apt-get update && apt-get -qqy install make git

ADD kind /usr/local/bin
ADD helm /usr/local/bin
ADD repo /usr/local/bin

RUN curl -Lo - https://dl.google.com/go/go1.11.5.linux-amd64.tar.gz | tar -C /usr/local -xzf -
