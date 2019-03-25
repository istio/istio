FROM  golang:1.11

ARG     HELM_VER=v2.11.0
ARG     K8S_VER=v1.11.1

# Environment variables used in the build.
ENV     GOPATH=/go
ENV     PATH=/go/bin:/go/out/linux_amd64:$PATH

# Include minikube and kubectl in the image
RUN curl -Lo /tmp/kubectl https://storage.googleapis.com/kubernetes-release/release/${K8S_VER}/bin/linux/amd64/kubectl && \
    chmod +x /tmp/kubectl && mv /tmp/kubectl /usr/local/bin/

# Install helm
RUN cd /tmp && mkdir /install && \
    curl -Lo /tmp/helm.tgz https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-linux-amd64.tar.gz && \
    tar xfz helm.tgz && \
    mv linux-amd64/helm /usr/local/bin && \
    rm -rf helm.tgz linux-amd64

RUN curl -s https://get.docker.io/ | sh

RUN go get -u sigs.k8s.io/kind


ADD . /install
