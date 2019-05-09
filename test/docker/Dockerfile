# Image for building and running Kind
#
# Kind will be used in a CI/CD system or for local testing.
#
# Already present: kubectl, kubeadm, ubuntu
#
# make docker.istio-builder

############################################
# Image to pull docker binary
FROM docker:latest as docker

############################################
# Image to create go binaries
FROM golang:1.12.5 as golang
ENV     GO111MODULE=on
ENV     GOPROXY=https://proxy.golang.org

# IMPORTANT: kind releases may or may not work with the 1.14.1 kindest/node tag.
# We want to create images with multiple k8s versions.
# Use latest since there is not a specific release to match the current kindest/node:v1.14.1 image
RUN GO111MODULE="on" go get -u sigs.k8s.io/kind@master

# Used to upload test results to test grid
RUN go get -u istio.io/test-infra/toolbox/ci2gubernator
RUN go get -u github.com/jstemmer/go-junit-report

# get helm binary - do here to limit to only binary in final image
RUN mkdir tmp
RUN curl -Lo - https://storage.googleapis.com/kubernetes-helm/helm-v2.13.1-linux-amd64.tar.gz | (cd tmp; tar -zxvf -)

# do repo for consistency - doesn't pull extra..
RUN curl https://storage.googleapis.com/git-repo-downloads/repo > /usr/local/bin/repo
RUN chmod +x /usr/local/bin/repo

# create istioctl from `master`, assuming that's what the builder had locally
# ignores `The command '/bin/sh -c go get -d istio.io/istio' returned a non-zero code: 1`
RUN go get -d istio.io/istio || true   
RUN cd /go/src/istio.io/istio && make istioctl

############################################
# Main image
FROM  kindest/node:v1.14.1

# Environment variables used in the build.
ENV     GOROOT=/usr/local/go
ENV     PATH=/usr/local/go/bin:/bin:/usr/bin:${PATH}

RUN  apt-get update && apt-get -qqy install make git

RUN curl -Lo - https://dl.google.com/go/go1.12.5.linux-amd64.tar.gz | tar -C /usr/local -xzf -

# It appears go test in istio/istio requires gcc
RUN  apt-get -qqy install build-essential autoconf libtool autotools-dev

# Copy from prior stages
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker

COPY --from=golang /go/bin/kind /usr/local/bin/kind
COPY --from=golang /go/bin/ci2gubernator /usr/local/bin/ci2gubernator
COPY --from=golang /go/bin/go-junit-report /usr/local/bin/go-junit-report
COPY --from=golang /go/tmp/linux-amd64/helm /usr/local/bin/helm
COPY --from=golang /usr/local/bin/repo /usr/local/bin/repo
COPY --from=golang /go/out/linux_amd64/release/istioctl /usr/local/bin/istioctl
