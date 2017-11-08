FROM    circleci/golang:1.9

RUN     go get github.com/coreos/etcd/cmd/etcd

ARG     K8S_VER=v1.7.4
ARG     MASTER_IP=127.0.0.1
ARG     MASTER_CLUSTER_IP=10.99.0.1
RUN     mkdir -p /tmp/apiserver && \
        cd /tmp/apiserver && \
        wget https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kube-apiserver && \
        chmod +x /tmp/apiserver/kube-apiserver

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

ENTRYPOINT ["/tmp/apiserver/start-test-server.sh"]