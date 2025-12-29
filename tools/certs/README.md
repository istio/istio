# Generating Certificates for Bootstrapping Multicluster / Mesh Expansion Chain of Trust

The directory contains two Makefiles for generating new root, intermediate certificates and workload certificates:
- `Makefile.k8s.mk`: Creates certificates based on a root-ca from a k8s cluster. The current context in the default
`kubeconfig` is used for accessing the cluster.
- `Makefile.selfsigned.mk`: Creates certificates based on a generated self-signed root.

The table below describes the targets supported by both Makefiles.

Make Target | Makefile | Description
------ | -------- | -----------
`root-ca` | `Makefile.selfsigned.mk` | Generates a self-signed root CA key and certificate.
`fetch-root-ca` | `Makefile.k8s.mk` | Fetches the Istio CA from the Kubernetes cluster, using the current context in the default `kubeconfig`.
`$NAME-cacerts` | Both | Generates intermediate certificates signed by the root CA for a cluster or VM with `$NAME` (e.g., `us-east`, `cluster01`, etc.). They are stored under `$NAME` directory. To differentiate between clusters, we include a `Location` (`L`) designation in the certificates `Subject` field, with the cluster's name.
`$NAMESPACE-certs` | Both | Generates intermediate certificates and sign certificates for a virtual machine connected to the namespace `$NAMESPACE` using serviceAccount `$SERVICE_ACCOUNT` using the root cert and store them under `$NAMESPACE` directory.
`clean` | Both | Removes any generated root certificates, keys, and intermediate files.

For example:

```bash
make -f Makefile.selfsigned.mk root-ca
```

Note that the Makefile generates long-lived intermediate certificates. While this might be
acceptable for demonstration purposes, a more realistic and secure deployment would use
short-lived and automatically renewed certificates for the intermediate CAs.

## Creating Certificates Using an Existing Istio CA

```bash
make -f Makefile.k8s.mk fetch-root-ca
```

The `fetch-root-ca` target retrieves the root CA certificate and key from an Istio-enabled Kubernetes cluster. This process is useful when establishing a trusted certificate chain across multiple clusters or environments using an existing Istio root certificate. **By default, it fetches the certificate and key from the `istio-ca-secret`, and if that is not available, it retrieves them from the `cacerts` secret.**

The command generates the certificate and key files in a directory named after the **current context** from your `kubeconfig` file.

```bash
make -f Makefile.k8s.mk $(cluster name)-cacerts
```

Afterwards, running the above command will generate an **Intermediate CA** certificate based on the root CA. For example, if you want to create an intermediate CA for `cluster01`, you would run the following command:

```bash
make -f Makefile.k8s.mk cluster01-cacerts
```
