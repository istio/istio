#!/bin/sh

# This script implements a workaround for enabling dynamic external
# webhooks on GKE as described by
# https://github.com/kubernetes/kubernetes/issues/49987#issuecomment-319739227
#
# (1) Create service with type=LoadBalancer and selector that maps to
# webhook deployment.
#
# (2) Create another service with externalIPs set to the LB IP allocated in step (1).
#
# (3) Manually create endpoint object with LB IP
#
# (4) Generate self-signed CA and server cert/key with LB IP.
#
# (5) Create k8s secret with CA and server cert/key. Webhook
# deployment should watch for creation of this secret and register
# itself with k8s apiserver when secret becomes available.

set -e

function usage() {
    cat <<EOF
Generate wrapper external service and corresponding certificates for
External Admission Webhook on GKE (see https://github.com/istio/pilot/issues/1164).

usage: ${0} [OPTIONS]

The following flags are required.

       --service-name     Service to wrap. Service type will be changed to type=LoadBalancer.
       --secret-name      Secret name for self-signed CA certificate and server certificate/key pair.
       --namespace        Namespace where the wrappedservice resides. The secret is also created in this namespace.
       --port             Port of the admission webhook.

EOF
    exit 1
}

while [[ $# -gt 0 ]]; do
    case ${1} in
        --service-name)
            serviceName="$2"
            shift
            ;;
        --secret-name)
            secretName="$2"
            shift
            ;;
        --namespace)
            namespace="$2"
            shift
            ;;
        --port)
            port="$2"
            shift
            ;;
        *)
            usage
            ;;
    esac
    shift
done

[ -z ${serviceName} ] && usage
[ -z ${secretName} ] && usage
[ -z ${namespace} ] && usage
[ -z ${port} ] && usage

CN_BASE=${serviceName}
tmpdir=$(mktemp -d)
serviceNameExternal=${serviceName}-external

serviceType=$(kubectl -n ${namespace} get service ${serviceName} -o=jsonpath='{.spec.type}')
if [ "${serviceType}" != "LoadBalancer" ]; then
    kubectl -n ${namespace} patch service ${serviceName} -p '{"spec":{"type":"LoadBalancer"}}'
fi

echo -n "Waiting for LoadBalancer IP to be ready .."
while true; do
    ip=$(kubectl -n ${namespace} get service ${serviceName} -o=jsonpath='{.status.loadBalancer.ingress[*].ip}')
    if [ ! -z ${ip} ]; then
        break
    fi
    sleep 1
    echo -n "."
done
echo " ${ip}"


cat <<EOF | kubectl -n ${namespace} apply -f -
apiVersion: v1
kind: Service
metadata:
  name: ${serviceNameExternal}
spec:
  externalIPs:
  - ${ip}
  ports:
  - port: ${port}
---
apiVersion: v1
kind: Endpoints
metadata:
  name: ${serviceNameExternal}
subsets:
- addresses:
  - ip: ${ip}
  ports:
  - port: ${port}
EOF

cat > ${tmpdir}/webhook.conf <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth, serverAuth
subjectAltName = @alt_names
[alt_names]
IP.1 = ${ip}
EOF


echo "Creating temp certs under ${tmpdir}"

# Create self-signed CA certiticate
openssl genrsa -out ${tmpdir}/ca-key.pem 2048
openssl req -x509 -new -nodes -key ${tmpdir}/ca-key.pem -days 100000 -out ${tmpdir}/ca-cert.pem -subj "/CN=${CN_BASE}_ca"

# Create a server certiticate
openssl genrsa -out ${tmpdir}/server-key.pem 2048
openssl req -new -key ${tmpdir}/server-key.pem -out ${tmpdir}/server.csr -subj "/CN=${CN_BASE}_server" -config ${tmpdir}/webhook.conf
openssl x509 -req -in ${tmpdir}/server.csr -CA ${tmpdir}/ca-cert.pem -CAkey ${tmpdir}/ca-key.pem -CAcreateserial \
        -out ${tmpdir}/server-cert.pem -days 100000 -extensions v3_req -extfile ${tmpdir}/webhook.conf

kubectl create -n ${namespace} secret generic ${secretName} \
        --from-file=${tmpdir}/ca-cert.pem \
        --from-file=${tmpdir}/server-key.pem \
        --from-file=${tmpdir}/server-cert.pem
