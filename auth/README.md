# Istio Authentication Module

The idea of [service
mesh](https://docs.google.com/document/d/1RRPrDK0mEwhPb13DSyF6pODugrRTFLAXia9CZLPoQno/edit)
has been proposed that injects high-level networking functionality in
Kubernetes’ deployments by interposing Istio proxies in a transparent or
semi-transparent fashion. Istio auth leverages Istio proxies to enable strong
authentication and data security for the services’ inbound and outbound
traffic, without or with little change to the application code.

## Goals
- Secure service to service communication and end-user to service communication
  via Istio proxies.
- Provide a key management system to automate key generation, distribution, and
  rotation.
- Expose the authenticated identities for authorization, rate limiting,
  logging, monitoring, etc.
