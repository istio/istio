# Generating Certificates for Bootstrapping Multicluster / Mesh Expansion Chain of Trust

The directory contains a `Makefile` for generating new root and intermediate certificates.
The following `make` targets are defined:

- `make root-ca`: this will generate a new root CA key and certificate.
- `make $NAME-certs`: this will generate all needed files to bootstrap a new Citadel for cluster `$NAME` (e.g., `us-east`, `cluster01`, etc.).
- `make $NAME-certs-wl`: this will generate certificates for a virtual machine connected to the namespace `$NAMESPACE` using
serviceAccount `$SERVICE_ACCOUNT` as well as intermediate certificates for K8s cluster `$NAME`

The intermediate CA files used for cluster `$NAME` are created under a directory named
`$NAME`. By creating files under a directory, we can create them using the naming convention
expected by Citadel's command line options. To differentiate between clusters, we include a
`Location` (`L`) designation in the certificates `Subject` field, with the cluster's name.
Similarly, we set the `Subject Alternate Name` field to include the cluster name as part
of the SPIFFE identity.

Note that the Makefile generates long lived intermediate certificates. While this might be
acceptable for demonstration purposes, a more realistic and secure deployment would use short
lived and automatically renewed certificates for the intermediate Citadels.
