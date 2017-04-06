---
bodyclass: docs
headline: Architecture
layout: docs
sidenav: doc-side-concepts-nav.html
title: Architecture
type: markdown
---

The Istio service mesh consists of three major components:

## Proxy

The Istio proxy is designed to mediate inbound and outbound
traffic for all Istio-managed services. The Istio proxy is based on
[Envoy](https://lyft.github.io/envoy/). Istio leverages Envoy's features
such as dynamic service discovery, load balancing, TLS termination, HTTP/2 & gRPC
proxying, circuit breakers, health checks, staged rollouts with %-based
traffic split, fault injection, and a rich set of metrics. In addition,
Istio extends the proxy to interact with the mixer to enforce various
access control policies rate limiting, ACLs, as well as telemetry
reporting.

## Mixer

The Istio mixer is responsible for enforcing access control
and usage policies across the service mesh and collects telemetry data from
proxies and istio-managed services alike. The Istio proxy extracts request
level attributes that are then evaluated by the mixer. More info on the
attribute extraction and policy evaluation can be found
[here](../reference/attributes.md). The mixer includes a flexible plugin model enabling
it to interface to a variety of host environments and configured backends,
abstracting the proxy and Istio-managed services from these details.

## Manager

The Istio manager serves as an interface between the user
and Istio, collecting configuration, validating it and propagating it to
various components. It abstracts platform-specific implementation details
from the mixer and proxies, providing them with an
[abstract representation](model.md) of user's services that is independent
of the underlying platform. In addition, [traffic management rules](../reference/rule-dsl.md)
(i.e. generic layer-4 rules and layer-7 HTTP/gRPC routing rules)
can be programmed at runtime via the Istio Manager.

<img src="../../img/arch.svg" alt="The overall architecture of an Istio-based service.">

<div id="toc" class="toc mobile-toc"></div>
