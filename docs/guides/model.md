---
bodyclass: docs
headline: Service Model
layout: docs
sidenav: doc-side-guides-nav.html
title: Service Model
type: markdown
---

The Istio manager serves as an interface between the user and Istio,
collecting configuration, validating it and propagating it to various
components. It abstracts platform-specific implementation details from the
mixer and proxies, providing them with an abstract representation of user's
services that is independent of the underlying platform. The model
described below assumes a microservice-based application owned by a single
tenant.

**Istio-managed service:** Modern cloud applications are created by
composing together independent sets of services. Individual services are
referenced by their fully-qualified domain name (FQDN) and one or more
ports where the service is listening for connections.

**Platform-agnostic internal representation:** The Istio model of a
microservice is independent of how it is represented in the underlying
platform (Kubernetes, Mesos, CloudFoundry, etc.). Platform-specific
adapters are responsible for populating the internal model representation
with various fields, from the metadata found in the platform.

## Multiple versions of a service

Istio introduces the concept of a service version, which is a finer-grained
way to subdivide service instances by versions (`v1`, `v2`) or environment
(`staging`, `prod`). These variants are not necessarily different API
versions: they could be iterative changes to the same service, deployed in
different environments (prod, staging, dev, etc.). Common scenarios where
this occurs include A/B testing, canary rollouts, etc. Istio
[routing rules](../reference/rule-dsl.md) can refer to the service versions, to provide
additional control over traffic between services.

**Tags** Each version of a service can be differentiated by a unique set of
tags associated with the version. Tags are simple key value pairs
assigned to the instances of a particular service version, i.e., all
instances of the same version must have the same tags.

## Populating the abstract service model

Where possible, Istio leverages the service registration and discovery
mechanism provided by the underlying platform to populate its abstract
service model. Most container platforms come built-in with a service
registry (e.g., kubernetes, mesos) where a pod specification can contain
all the version related tags. Upon launching the pod, the platform
automatically registers the pod with the registry along with the tags.  In
other platforms, a dedicated service registration agent might be needed to
automatically register the service with a service registration/discovery
solution like Consul, etc.

At the moment, Istio integrates readily with the Kubernetes service registry
and automatically discovers various services, their pods etc., and
groups the pods into unique sets -- each set representing a service
version. In future, Istio will add support for pulling in similar
information from Mesos registry and *potentially* other registries.

## Communication between services

Clients of a service have no knowledge of different versions of the
service. They can continue to access the services using the hostname/IP
address of the service. The Istio Proxy intercepts and forwards all
requests/responses between the client and the service.

The actual choice of the service version is determined dynamically by the
Istio Proxy based on the routing rules set forth by the operator. This
model enables the application code to decouple itself from the
evolution of its dependent services, while providing other benefits as well
(see [mixer](mixer.md)). Routing rules allow the proxy to select a version based on
criterion such as (headers, url, etc.), tags associated with
source/destination and/or by weights assigned to each version.

Note that Istio does not provide a DNS. Applications can try to resolve the
FQDN using the DNS service present in the underlying platform (kube-dns,
mesos-dns, etc.).

<div id="toc" class="toc mobile-toc"></div>
