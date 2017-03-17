# Overview

As enterprises migrate from traditional VM infrastructures to more agile
microservice-based deployments on container platforms, there is a need for
a simplifying technology that can take care of common cross-cutting
capabilities around service communication and management such as secure
interconnect, service discovery & load balancing, staged rollouts, A/B
testing, intelligent rate limiting, authentication, access control,
monitoring, logging, etc.

Broadly speaking, an _open, platform independent service mesh_ that
simplifies traffic management, policy enforcement, and telemetry collection
has two main benefits. It allows application developers to focus on the
business logic and iterate quickly on new features by managing how traffic
flows across their services. It simplifies the operators job of enforcing
various policies and monitor the mesh from a central control point,
independent of the evolution of the application ensuring continuous
compliance with policies of the organization/business unit.

## Architecture

The Istio service mesh consists of three major components:

- **Proxy**. The Istio proxy is designed to mediate inbound and outbound
traffic for all Istio-managed services. The Istio proxy is based on
[Envoy](https://lyft.github.io/envoy/) with extensions to fit within the
Istio service mesh. Envoy has been operational in production at Lyft at
large scale (over 10,000 VMs, handling 100+ services). 

Istio leverages Envoy's features such as service discovery, load balancing,
TLS termination, HTTP/2 & gRPC proxying, circuit breakers, health checks,
staged rollouts with %-based traffic split, fault injection, and a rich set
of metrics. In addition, Istio extends the proxy to interact with the mixer
to enforce various access control policies rate limiting, ACLs, as well as
telemetry reporting.

- **Mixer**. The Istio mixer is responsible for enforcing access control
and usage policies across the service mesh and collects telemetry data from
proxies and istio-managed services alike. The Istio proxy extracts request
level attributes that are then evaluated by the mixer. More info on the
attribute extraction and policy evaluation can be found
[here](attributes.md). The mixer includes a flexible plugin model enabling
it to interface to a variety of host environments and configured backends,
abstracting the proxy and Istio-managed services from these details.

- **Manager**. The Istio manager serves as an interface between the user
and Istio, collecting configuration, validating it and propagating it to
various components. It abstracts platform specific implementation details
from the mixer and proxies, providing them with an
[abstract representation](model.md) of user's services that is independent
of the underlying platform. In addition, traffic management rules
(i.e. layer-7 routing rules) can be programmed at runtime via the Istio
Manager.

<figure id="fig-arch" class="center">
<img src="../images/arch.png" alt="The overall architecture of an Istio-based service.">
</figure>

## Further Reading

The following pages describe individual aspects of Istio.

1. [Abstract Service Model](model.md)
2. [Attributes & Policy Evaluation](attributes.md)
2. [Traffic Management (Layer-7 Routing)](rule-dsl.md)
3. [Istio Mixer](mixer.md)
4. [Istioctl CLI Manual](istioctl.md)
