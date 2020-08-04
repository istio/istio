# Istio plugin CA sample certificates

This directory contains sample pre-generated certificate and keys to demonstrate how an operator could configure Citadel with an existing root certificate, signing certificates and keys. In such
a deployment, Citadel acts as an intermediate certificate authority (CA), under the given root CA.
Instructions are available [here](https://istio.io/docs/tasks/security/cert-management/plugin-ca-cert/).

The included sample files are:

- `root-cert.pem`: root CA certificate.
- `ca-cert.pem` and `ca-cert.key`: Citadel intermediate certificate and corresponding private key.
- `cert-chain.pem`: certificate trust chain.
