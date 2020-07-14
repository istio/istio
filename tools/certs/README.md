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