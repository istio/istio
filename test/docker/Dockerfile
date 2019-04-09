# Image for building and running Kind
# Kind will be used in a CI/CD system
# Already present: kubectl, kubeadm, ubuntu, docker
FROM  kindest/node:latest

# Environment variables used in the build.
ENV     GOPATH=/go
ENV     PATH=/go/bin:/go/out/linux_amd64:$PATH

# Need to have docker installed
#RUN curl -s -L https://get.docker.io/ | sh -x

ADD kind /usr/local/bin
ADD helm /usr/local/bin

RUN  apt-get update

# Install fpm tool - for deb/rpm building
RUN apt-get -qqy install ruby ruby-dev rubygems build-essential autoconf libtool autotools-dev && \
     gem install --no-ri --no-rdoc fpm


RUN apt-get -qqy install make git

ADD repo /usr/local/bin
