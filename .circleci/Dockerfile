FROM    circleci/golang:1.9

# The base circleci image runs as user 'circleci'(3434), with sudo capabilities.
# Based on Debian9. Go installed in /usr/local/go

# Env:
# GOLANG_VERSION
# GPATH=/go

# Workdir: /go

# Also installed docker, docker-compose, dockerize, jq

ARG     K8S_VER=v1.7.4
ARG     MASTER_IP=127.0.0.1
ARG     MASTER_CLUSTER_IP=10.99.0.1

ARG     ETCD_VER=v3.2.15
RUN     cd /tmp && \
        curl -L -o etcd.tgz https://github.com/coreos/etcd/releases/download/${ETCD_VER}/etcd-${ETCD_VER}-linux-amd64.tar.gz && \
        tar xzf etcd.tgz && \
        sudo mv etcd-${ETCD_VER}-linux-amd64/etcd /usr/local/bin/etcd && \
        rm -rf etcd*

RUN     mkdir -p /tmp/apiserver && \
        cd /tmp/apiserver && \
        wget https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kube-apiserver && \
        chmod +x /tmp/apiserver/kube-apiserver

# TODO: generate and checkin the test apiserver and istio-ca keys
RUN     cd /tmp && \
        curl -L -O https://storage.googleapis.com/kubernetes-release/easy-rsa/easy-rsa.tar.gz && \
        tar xzf easy-rsa.tar.gz && \
        cd easy-rsa-master/easyrsa3 && \
        ./easyrsa init-pki && \
        ./easyrsa --batch "--req-cn=${MASTER_IP}@`date +%s`" build-ca nopass && \
        ./easyrsa --subject-alt-name="IP:${MASTER_IP},""IP:${MASTER_CLUSTER_IP},""DNS:kubernetes,""DNS:kubernetes.default,""DNS:kubernetes.default.svc,""DNS:kubernetes.default.svc.cluster,""DNS:kubernetes.default.svc.cluster.local" --days=10000 build-server-full server nopass && \
        cp /tmp/easy-rsa-master/easyrsa3/pki/ca.crt /tmp/apiserver/ca.crt && \
        cp /tmp/easy-rsa-master/easyrsa3/pki/issued/server.crt /tmp/apiserver/server.crt && \
        cp /tmp/easy-rsa-master/easyrsa3/pki/private/server.key /tmp/apiserver/server.key && \
        cd /tmp && \
        rm -rf /tmp/easy-rsa-master/

RUN     go get -u github.com/golang/dep/cmd/dep

ADD     start-test-server.sh /tmp/apiserver/start-test-server.sh

# Tool used to convert 'go test' to junit, for integration with CI dashboard
RUN     go get github.com/jstemmer/go-junit-report

# Install fpm tool
RUN  sudo apt-get -qqy install ruby ruby-dev rubygems build-essential && \
     sudo gem install --no-ri --no-rdoc fpm

# Include minikube and kubectl in the image
RUN curl -Lo /tmp/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kubectl && \
    chmod +x /tmp/kubectl && sudo mv /tmp/kubectl /usr/local/bin/

RUN curl -Lo /tmp/minikube https://storage.googleapis.com/minikube/releases/v0.22.3/minikube-linux-amd64 &&\
    chmod +x /tmp/minikube && sudo mv /tmp/minikube /usr/local/bin/

