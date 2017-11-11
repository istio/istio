#! /bin/sh

${GOPATH}/bin/etcd &

/tmp/apiserver/kube-apiserver --etcd-servers http://127.0.0.1:2379 \
    --client-ca-file /tmp/apiserver/ca.crt \
    --requestheader-client-ca-file /tmp/apiserver/ca.crt \
    --tls-cert-file /tmp/apiserver/server.crt \
    --tls-private-key-file /tmp/apiserver/server.key \
    --service-cluster-ip-range 10.99.0.0/16 \
    --port 8080 -v 2 --insecure-bind-address 0.0.0.0