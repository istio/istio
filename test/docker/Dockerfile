# Image for building and running Kind
# Kind will be used in a CI/CD system
# Already present: kubectl, kubeadm, ubuntu
#
# make docker.istio-builder

# Image to pull docker binary
FROM docker:latest as docker

# Image to create go binaries
FROM golang:1.11.5 as golang
RUN go get -u sigs.k8s.io/kind
RUN go get -u istio.io/test-infra/toolbox/ci2gubernator
RUN go get -u github.com/jstemmer/go-junit-report

#get helm binary - do here to limit to only binary in final image
RUN mkdir tmp
RUN curl -Lo - https://storage.googleapis.com/kubernetes-helm/helm-v2.13.1-linux-amd64.tar.gz | (cd tmp; tar -zxvf -)

# do repo for consistency - doesn't pull extra..
RUN curl https://storage.googleapis.com/git-repo-downloads/repo > /usr/local/bin/repo
RUN chmod +x /usr/local/bin/repo

#Main image
FROM  kindest/node:v1.14.1

# Environment variables used in the build.
ENV     GOROOT=/usr/local/go
ENV     PATH=/usr/local/go/bin:/bin:/usr/bin:${PATH}

RUN  apt-get update && apt-get -qqy install make git

RUN curl -Lo - https://dl.google.com/go/go1.11.5.linux-amd64.tar.gz | tar -C /usr/local -xzf -

# It appears go test in istio/istio requires gcc
RUN  apt-get -qqy install build-essential autoconf libtool autotools-dev

# The linux version of these were built locally and pashed in.
# Move to another stage?
ADD istioctl /usr/local/bin

# Copy from prior stages
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker

COPY --from=golang /go/bin/kind /usr/local/bin/kind
COPY --from=golang /go/bin/ci2gubernator /usr/local/bin/ci2gubernator
COPY --from=golang /go/bin/go-junit-report /usr/local/bin/go-junit-report
COPY --from=golang /go/tmp/linux-amd64/helm /usr/local/bin/helm
COPY --from=golang /usr/local/bin/repo /usr/local/bin/repo
