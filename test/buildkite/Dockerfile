# Image based on buildkite docker agent, with the tools used for Istio installer and tests.
# This will run in a dockerfile or k8s - 'make test' should work.
# Image to create go binaries
FROM golang:1.12.5 as golang
RUN GO111MODULE="on" go get -u sigs.k8s.io/kind@master

#get helm binary - do here to limit to only binary in final image
RUN mkdir tmp
RUN curl -Lo - https://storage.googleapis.com/kubernetes-helm/helm-v2.13.1-linux-amd64.tar.gz | (cd tmp; tar -zxvf -)

# do repo for consistency - doesn't pull extra..
RUN curl https://storage.googleapis.com/git-repo-downloads/repo > /usr/local/bin/repo
RUN chmod +x /usr/local/bin/repo

FROM buildkite/agent:3-ubuntu

# Environment variables used in the build.
ENV     GOROOT=/usr/local/go
ENV     PATH=/usr/local/go/bin:/bin:/usr/bin:${PATH}

RUN  apt-get update && apt-get -qqy install make git

RUN curl -Lo - https://dl.google.com/go/go1.12.5.linux-amd64.tar.gz | tar -C /usr/local -xzf -

# It appears go test in istio/istio requires gcc
RUN  apt-get -qqy install build-essential autoconf libtool autotools-dev

COPY --from=golang /go/bin/kind /usr/local/bin/kind
COPY --from=golang /go/tmp/linux-amd64/helm /usr/local/bin/helm
COPY --from=golang /usr/local/bin/repo /usr/local/bin/repo