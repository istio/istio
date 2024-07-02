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

## Trusted roots

Istio CA gRPC returns the CA roots from cacerts secret or the self generated secret as last element. 

Istiod can also get roots from ProxyConfig and /var/run/secrets/istiod/ca (mounted from
istio-ca-root-cert cert map in istiod, different in sidecars - only file mount supported). The certmap is created by Istiod by concatenating all roots.

/etc/certs/root-certs.pem is used if pilotCertProvider is not K8S or Istiod - but doesn't 
appear to be mounted in the default installer. May be used when Istio runs on a VM.


## Opinionated setups for CA

CA has many options, depending on the security requirements and the existing environment of the user. From easiest to most secure:

1. Simplest: use the default istio-ca-secret. Append roots to the root-cert.pem key to support extra roots or for rotations. Doesn't work well with multiple clusters, but best for dev/testing. It is possible to use a tool to patch the root-cert.pem in each cluster with the merged roots - but if you have automation better use (2).

2. Use cacerts, single common root and each cluster gets its own keys: use CertManager or other tools to generate cacerts for each cluster, with the top root offline or in a separate cluster. 

3. More advanced: customize istiod install to use a CSI volume or separate container to maintain /etc/cacerts. No secrets with the private keys stored in K8S, but Istiod has access to a root key and can sign any identity. Same as (2) except no CA secrets in K8S.

4. Istiod acting only as a RA - using either K8S or another Istio CA gRPC signer. No Istio secrets holding root CAs, Istiod doesn't have access to CA secrets but can mint certificates. Istiod still handles CSR verification and can support ambient special certs. The CA may impose additional restrictions on Istiod to limit the identities it can generate.

5. Fully external using Istio CA protocol - Istiod CA disabled, an external CA_ADDR used in sidecars for all certs and ztunnel. The signer will need to understand Ambient impersonation.

6. Fully external using host providers - istio-agent will load from /var/run/secrets/workload-spiffe-credentials, platform provided certificates. The agent can also load from /var/run/secrets/workload-spiffe-uds and credential-uds - with a host provided UDS socket. Neither work currently with ambient.

TODO: include the install options for each option.


## Istiod with external DNS certs

Default is for Istiod to sign its own certificate, but volume mounts are also possible, using istio-tls and 
istio-ca-root-cert. 

When Istiod is running on a VM or locally, this corresponds to:

- ./var/run/secrets/istiod/ca: root-cert.pem used for client cert verification. 
- ./var/run/secrets/istiod/tls: tls.crt, tls.key are used for Istiod certificate. If root-cert.pem is missing - ca.crt 
  is checked here

If the files are used, they take priority - cacerts secret (./etc/cacerts) is only used for signing certificates, the
ca.crt in ./etc/cacerts is ignored.

The code in 'namespacecontroller.go' creates istio-ca-root-cert configmap in each namespace, replicating the bundle in 
keycertbundle/watcher.go - which is looking first for the istio-tls secret (by checking the path).

*If Istio custom certs are used, the root CA public is loaded from the Istiod certs - not the Istiod CA*

That's using:
- /var/run/secrets/istiod/tls/tls.crt, tls.key, ca.crt or root-cert.pem
- or tlsCertFile, tlsKeyFile, caCertFile in istiod command line

This mode has priority - if the files are present, the 'cacerts' ca.crt is not distributed
or used.
