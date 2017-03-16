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

- **Proxy**. The Istio proxy is designed to mediate inbound and outbound traffic for all Istio-managed services. It enforces
access control and usage policies, and provides rich routing, load balancing, and protocol conversion. The Istio proxy is based on 
[Envoy](https://lyft.github.io/envoy/) with extensions to fit within the Istio service mesh.

- **Mixer**. The Istio mixer is the nexus of the Istio service mesh. The proxy delegates policy decisions to the mixer, and both the
proxy and Istio-managed services direct all telemetry data to the mixer. The mixer includes a flexible plugin model enabling it
to interface to a variety of host environments and configured backends, abstracting the proxy and Istio-managed services
from these details.

- **Configuration Manager**. The Istio manager is used to configure Istio deployments and propagate configuration to 
the other components of the system, including the Istio mixer and the Istio 
proxies deployed in the mesh.

<figure id="fig-arch" class="center">
<img src="../images/arch.png" alt="The overall architecture of an Istio-based service.">
</figure>

## Further Reading

The following pages describe individual aspects of Istio.

1. [Abstract Service Model](model.md)
2. [Istio Routing Rules Specification](rule-dsl.md)
3. [Istio Mixer](mixer.md)
4. [Istioctl CLI Manual](istioctl.md)
