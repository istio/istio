# Generating Certificates for Istiod

Best practice is to have the root CA in a secure, restricted storage, and use it to generate 
shorter-lived intermediate CAs for each Istio cluster that is configured to issue workload certificates,
for example the central cluster(s).

This is an example - if you have an existing CA, use this to create the CSR, and sign it using your
CA process.

## Create or get a root CA
- `make root-ca`: will generate a new root CA key and certificate - in case you don't have one already. 

For migrating Istio to intermediary certificates:

- `make fetch-root-ca`: will fetch the existing root CA from the cluster. Files stored in CONTEXT/k8s-root-{key,cert}.pem
- `cp $CONTEXT/k8s-root.key.pem root-key.pem`
- `cp $CONTEXT/k8s-root.cert.pem root-cert.pem`

## Generate self-signed certs

- `make $NAME-cacerts-selfSigned`:  

# Generating Certificates for Bootstrapping Multicluster / Mesh Expansion Chain of Trust

The directory contains a `Makefile` for generating new root, intermediate certificates and workload certificates.
The following `make` targets are defined:

- `make fetch-root-ca`: this will fetch root CA  and key from a k8s cluster.
- `make root-ca`: this will generate a new root CA key and certificate.
- `make $NAME-cacerts-k8s`: this will generate intermediate certificates for a cluster or VM with $NAME
(e.g., `us-east`, `cluster01`, etc.). They are signed with istio root cert from the specified k8s cluster and are stored
under $NAME directory.
- `make $NAME-cacerts-selfSigned`: this will generate self signed intermediate certificates for $NAME
and store them under $NAME directory.
- `make <NAMESPACE>-certs-selfSigned`: this will generate intermediate certificates and sign certificates for a
virtual machine connected to the namespace $NAMESPACE using serviceAccount $SERVICE_ACCOUNT
using self signed root certs and store them under $NAMESPACE directory.

- `make <NAMESPACE>-certs-k8s`: this will generate intermediate certificates and sign certificates for a virtual machine
 connected to the namespace $NAMESPACE using serviceAccount $SERVICE_ACCOUNT using root cert from k8s cluster and store
 them under $NAMESPACE directory.

The intermediate CA files used for cluster `$NAME` are created under a directory named
`$NAME`.  To differentiate between clusters, we include a
`Location` (`L`) designation in the certificates `Subject` field, with the cluster's name.

Note that the Makefile generates long lived intermediate certificates. While this might be
acceptable for demonstration purposes, a more realistic and secure deployment would use short
lived and automatically renewed certificates for the intermediate Citadels.
