# Istio CA

## Initialization

Istiod provides multiple integration paths with external CAs as well as a 'built in' CA. 

There are 2 commonly used paths for loading the CA:

- from a K8S Secret - typically istio-ca-secret
- from files in - ./etc/cacerts (the dot allows Istiod to run without changes to the root filesystem - for example as a non-root user in a VM)

The Istio default install is mounting the optional cacerts Secret to ./etc/cacerts.

This may seem confusing and duplicative - but there are few reasons. The istio-ca-secret is the most convenient way to start Istio - if it is not found, Istiod will create a self-signed root and automatically rotate the certificate. The secret can be loaded from remote Istiod or from a development machine. 

The file is a bit more complicated - it is intended for advanced users who may use a separate root CA, not stored in
K8S, to generate shorter-lived, separate roots for each K8S cluster. This could also be addressed with a Secret - and it is the typical use case - but the file may also be populated by a script or be mounted from a CSI volume, avoiding the storage of the root private key in a K8S Secret. 

The code path for file-based certificate loading can also handle multiple roots and changing the root CA. The process is relatively complex - since rotations are not frequent, restarting istiod can be acceptable.

The file path - and the casecrets Secret - should be used with the standard K8S and CertManager naming conventions:

- tls.key - the private key, in PEM format
- tls.crt - the matching certificate and all intermediary keys, concatenated in PEM format
- ca.crt - the root used to sign the last certificate in the chain, as well as additional roots. 

