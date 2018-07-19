# Dependencies and linters for build:
FROM ubuntu:xenial
# Need gcc for -race test (and some linters though those work with CGO_ENABLED=0)
RUN apt-get -y update && \
  apt-get --no-install-recommends -y upgrade && \
  apt-get --no-install-recommends -y install ca-certificates curl make git gcc \
  libc6-dev apt-transport-https ssh
# Install both go1.10.3 and go1.8.7 so we don't become incompatible with 1.8
RUN curl -f https://storage.googleapis.com/golang/go1.8.7.linux-amd64.tar.gz | tar xfz - -C /usr/local
# Newer go have no problem with being installed in random directories without requiring GOROOT so we
# leave the old go in /usr/local and put the new one, despite being preferred, in a different root:
RUN mkdir /go1.10
RUN curl -f https://dl.google.com/go/go1.10.3.linux-amd64.tar.gz | tar xfz - -C /go1.10
ENV GOPATH /go
RUN mkdir -p $GOPATH/bin
# We do pick the latest go first in the path
ENV PATH /go1.10/go/bin:/usr/local/go/bin:$PATH:$GOPATH/bin
RUN go version # check it's indeed the version we expect
# This is now handled through dep and vendor submodule
# RUN go get -u google.golang.org/grpc
# Install dep
RUN curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
# Install meta linters
RUN go get -u github.com/alecthomas/gometalinter
RUN gometalinter -i -u
WORKDIR /go/src/istio.io
# Docker:
RUN curl -fsSL "https://download.docker.com/linux/ubuntu/gpg" | apt-key add
RUN echo "deb [arch=amd64] https://download.docker.com/linux/ubuntu xenial stable" > /etc/apt/sources.list.d/docker.list
RUN apt-get -y update
RUN apt-get install --no-install-recommends -y docker-ce
