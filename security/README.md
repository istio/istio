# Istio Security

Breaking down a monolithic application into atomic services offers various benefits, including better agility, better scalability
and better ability to reuse services.
However, microservices also have particular security needs:

- To defend against the man-in-the-middle attack, they need traffic encryption.

- To provide flexible service access control, they need mutual TLS and fine-grained access policies.

- To audit who did what at what time, they need auditing tools.

Istio Security tries to provide a comprehensive security solution to solve all these issues.

This page gives an overview on how you can use Istio security features to secure your services, wherever you run them.
In particular, Istio security mitigates both insider and external threats against your data, endpoints, communication and platform.

![overview](https://cdn.rawgit.com/istio/istio/master/security/overview.svg)

The Istio security features provide strong identity, powerful policy, transparent TLS encryption, and authentication, authorization
and audit (AAA) tools to protect your services and data. The goals of Istio security are:

- **Security by default**: no changes needed for application code and infrastructure

- **Defense in depth**: integrate with existing security systems to provide multiple layers of defense

- **Zero-trust network**: build security solutions on untrusted networks

## High-level architecture

Security in Istio involves multiple components:

- **Citadel** for key and certificate management

- **Sidecar and perimeter proxies** to implement secure communication between clients and servers

- **Pilot** to distribute policies to the proxies

- **Mixer** to manage authorization and auditing

![architecture](https://cdn.rawgit.com/istio/istio/master/security/architecture.svg)

To find more information like Istio identity, PKI, Authentication, and Authorization, please check our
[website](https://istio.io/docs/concepts/security/).
