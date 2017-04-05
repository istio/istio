---
bodyclass: docs
headline: Mixer
layout: docs
sidenav: doc-side-guides-nav.html
title: Mixer
type: markdown
---

The mixer provides the control-plane abstractions necessary for most real-world multi-tenant services.
The proxy delegates policy decisions to the mixer and dispatches its telemetry data to the mixer, which
proceeds to repackage and redirect the data towards configured backends.

Services within the Istio mesh can also directly integrate with the mixer. For example, services may wish to provide rich telemetry for particular operations
beyond what the proxy automatically collects. Or services may use the mixer for resource-oriented quota management. Services that leverage the mixer in this 
way are abstracted from environment-specific control plane details, greatly easing the process of hosting the code in
different environments (different clouds & on-prem)

The mixer provides three core features:

**Precondition Checking**. Enables callers to verify a number of preconditions before responding to an incoming request from a service consumer. 
Preconditions can include whether the service consumer is properly authenticated, is on the service's whitelist, passes ACL checks, and more.

**Telemetry Reporting**. Enables services to produce logging, monitoring, tracing and billing streams intended for the service producer itself as well as 
for its consumers.

**Quota Management**. Enables services to allocate and free quota on a number of dimensions, Quotas are used as a relatively simple resource management 
tool to provide some fairness between service consumers when contending for limited resources.

## Mixer Adapters

Adapters are binary-level plugins to the mixer which make it possible to customize the mixer’s behavior. Adapters allow the mixer to interface 
to different backend systems that deliver core control-plane functionality, such as logging, monitoring, quotas, ACL checking, and more. Adapters
enable the mixer to expose a single consistent control API, independent of the backends in use. The exact set of adapters
used at runtime is determined through configuration.

<img src="../../img/adapters.svg" alt="Mixer and its adapters.">

<div id="toc" class="toc mobile-toc"></div>
