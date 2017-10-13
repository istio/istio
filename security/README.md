# Istio Authentication

[![Go Report Card](https://goreportcard.com/badge/github.com/istio/auth)](https://goreportcard.com/report/github.com/istio/auth)
[![codecov](https://codecov.io/gh/istio/auth/branch/master/graph/badge.svg)](https://codecov.io/gh/istio/auth)


## Overview

Istio Auth's aim is to enhance the security of microservices and their communication without requiring service code changes. It is responsible for:

*   Providing each service with a strong identity that represents its role to enable interoperability across clusters and clouds

*   Securing service to service communication and end-user to service communication

*   Providing a key management system to automate key and certificate generation, distribution, rotation, and revocation

## Architecture

The diagram below shows Istio Auth's architecture, which includes three primary components: identity, key management, and communication security. This diagram describes how Istio Auth is used to secure the service-to-service communication between service 'frontend' running as the service account 'frontend-team' and service 'backend' running as the service account 'backend-team'. Istio supports services running on both Kubernetes containers and VM/bare-metal machines.

![overview](https://cdn.rawgit.com/istio/auth/master/overview.svg)

As illustrated in the diagram, Istio Auth leverages secret volume mount to deliver keys/certs from Istio CA to Kubernetes containers. For services running on VM/bare-metal machines, we introduce a node agent, which is a process running on each VM/bare-metal machine. It generates the private key and CSR (certificate signing request) locally, sends CSR to Istio CA for signing, and delivers the generated certificate together with the private key to Envoy.

## Components

### Identity

Istio Auth uses [Kubernetes service accounts](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/) to identify who runs the service:

*   A service account in Istio has the format "spiffe://\<_domain_\>/ns/\<_namespace_>/sa/\<_serviceaccount_\>".
    *   _domain_ is currently _cluster.local_. We will support customization of domain in the near future.
    *   _namespace_ is the namespace of the Kubernetes service account.
    *   _serviceaccount_ is the Kubernetes service account name.

*   A service account is **the identity (or role) a workload runs as**, which represents that workload's privileges. For systems requiring strong security, the amount of privilege for a workload should not be identified by a random string (i.e., service name, label, etc), or by the binary that is deployed.

    *   For example, let's say we have a workload pulling data from a multi-tenant database. If Alice ran this workload, she will be able to pull a different set of data than if Bob ran this workload.

*   Service accounts enable strong security policies by offering the flexibility to identify a machine, a user, a workload, or a group of workloads (different workloads can run as the same service account).

*   The service account a workload runs as won't change during the lifetime of the workload.

*   Service account uniqueness can be ensured with domain name constraint

### Communication security

Service-to-service communication is tunneled through the client side [Envoy](https://envoyproxy.github.io/envoy/) and the server side Envoy. End-to-end communication is secured by:

*   Local TCP connections between the service and Envoy

*   Mutual TLS connections between proxies

*   Secure Naming: during the handshake process, the client side Envoy checks that the service account provided by the server side certificate is allowed to run the target service

### Key management

Istio v0.2 supports services running on both Kubernetes pods and VM/bare-metal machines. We use different key provisioning mechanisms for each scenario.

For services running on Kubernetes pods, the per-cluster Istio CA (Certificate Authority) automates the key & certificate management process. It mainly performs four critical operations :

*   Generate a [SPIFFE](https://spiffe.github.io/docs/svid) key and certificate pair for each service account

*   Distribute a key and certificate pair to each pod according to the service account

*   Rotate keys and certificates periodically

*   Revoke a specific key and certificate pair when necessary

For services running on VM/bare-metal machines, the above four operations are performed by Istio CA together with node agents.

## Workflow

The Istio Auth workflow consists of two phases, deployment and runtime. For the deployment phase, we discuss the two scenarios (i.e., in Kubernetes and VM/bare-metal machines) separately since they are different. Once the key and certificate are deployed, the runtime phase is the same for the two scenarios. We briefly cover the workflow in this section.

### Deployment phase (Kubernetes Scenario)

1.  Istio CA watches Kubernetes API Server, creates a [SPIFFE](https://spiffe.github.io/docs/svid) key and certificate pair for each of the existing and new service accounts, and sends them to API Server.

1.  When a pod is created, API Server mounts the key and certificate pair according to the service account using [Kubernetes secrets](https://kubernetes.io/docs/concepts/configuration/secret/).

1.  [Pilot]({{home}}/docs/concepts/traffic-management/pilot.html) generates the config with proper key and certificate and secure naming information,
which
 defines what service account(s) can run a certain service, and passes it to Envoy.

### Deployment phase (VM/bare-metal Machines Scenario)

1.  Adding service account for the service using Kubernetes annotation.

1.  Istio CA creates a gRPC service to take CSR request.

1.  Node agent creates the private key and CSR, sends the CSR to Istio CA for signing.

1.  Istio CA validates the credentials carried in the CSR, and signs the CSR to generate the certificate.

1.  Node agent puts the certificate received from CA and the private key to Envoy.

1.  The above CSR process repeats periodically for rotation.


### Runtime phase

1.  The outbound traffic from a client service is rerouted to its local Envoy.

1.  The client side Envoy starts a mutual TLS handshake with the server side Envoy. During the handshake, it also does a secure naming check to verify that the service account presented in the server certificate can run the server service.

1.  The traffic is forwarded to the server side Envoy after mTLS connection is established, which is then forwarded to the server service through local TCP connections.

## Best practices

In this section, we provide a few deployment guidelines and then discuss a real-world scenario.

### Deployment guidelines

*   If there are multiple service operators (a.k.a. [SREs](https://en.wikipedia.org/wiki/Site_reliability_engineering)) deploying different services in a cluster (typically in a medium- or large-size cluster), we recommend creating a separate [namespace](https://kubernetes.io/docs/tasks/administer-cluster/namespaces-walkthrough/) for each SRE team to isolate their access. For example, you could create a "team1-ns" namespace for team1, and "team2-ns" namespace for team2, such that both teams won't be able to access each other's services.

*   If Istio CA is compromised, all its managed keys and certificates in the cluster may be exposed. We *strongly* recommend running Istio CA on a dedicated namespace (for example, istio-ca-ns), which only cluster admins have access to.

### Example

Let's consider a 3-tier application with three services: photo-frontend, photo-backend, and datastore. Photo-frontend and photo-backend services are managed by the photo SRE team while the datastore service is managed by the datastore SRE team. Photo-frontend can access photo-backend, and photo-backend can access datastore. However, photo-frontend cannot access datastore.

In this scenario, a cluster admin creates 3 namespaces: istio-ca-ns, photo-ns, and datastore-ns. Admin has access to all namespaces, and each team only has
access to its own namespace. The photo SRE team creates 2 service accounts to run photo-frontend and photo-backend respectively in namespace photo-ns. The
datastore SRE team creates 1 service account to run the datastore service in namespace datastore-ns. Moreover, we need to enforce the service access control
in [Istio Mixer]({{home}}/docs/concepts/policy-and-control/mixer.html) such that photo-frontend cannot access datastore.

In this setup, Istio CA is able to provide keys and certificates management for all namespaces, and isolate microservice deployments from each other.

## Future work

*   Inter-cluster service-to-service authentication

*   Powerful authorization mechanisms: ABAC, RBAC, etc

*   Per-service auth enablement support

*   Secure Istio components (Mixer, Pilot)

*   End-user to service authentication using JWT/OAuth2/OpenID_Connect.

*   Support GCP service account

*   Unix domain socket for local communication between service and Envoy

*   Layer-7 Middle proxy support

*   Pluggable key management component
