# Sample Certificates for Citadel CA

This directory contains sample pre-generated certificate and keys to demonstrate how an operator could configure Citadel with an existing root certificate, signing certificates and keys. In such
a deployment, Citadel acts as an intermediate certificate authority (CA), under the given root CA.
Instructions are available [here](https://istio.io/docs/tasks/security/plugin-ca-cert/).

The included sample files are:

- `root-cert.pem`: root CA certificate.
- `ca-cert.pem` and `ca-cert.key`: Citadel intermediate certificate and corresponding private key.
- `cert-chain.pem`: certificate trust chain.

## Generating Certificates for Bootstrapping Multicluster Chain of Trust

Using the sample certificates to establish trust between multiple clusters is not recommended.
Since the directory is missing the root CA's private key, we're unable to create new certificates
for the clusters. Our only workable option is to use the **same** files for all clusters.
A better alternative would be to assign a different intermediate CA to each cluster, all signed by
a shared root CA. This will enable trust between workloads from different clusters as long as
they share a common root CA.

The directory contains a `Makefile` for generating new root and intermediate certificates.
The following `make` targets are defined:

- `make root-ca`: this will generate a new root CA key and certificate.
- `make $NAME-certs`: this will generate all needed files to bootstrap a new Citadel for cluster `$NAME` (e.g., `us-east`, `cluster01`, etc.).

The intermediate CA files used for cluster `$NAME` are created under a directory named
`$NAME`. By creating files under a directory, we can create them using the naming convention
expected by Citadel's command line options. To differentiate between clusters, we include a
`Location` (`L`) designation in the certificates `Subject` field, with the cluster's name.
Similarly, we set the `Subject Alternate Name` field to include the cluster name as part
of the SPIFFE identity.

Note that the Makefile generates long lived intermediate certificates. While this might be
acceptable for demonstration purposes, a more realistic and secure deployment would use short
lived and automatically renewed certificates for the intermediate Citadels.