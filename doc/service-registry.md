# Istio Service Model

Istio's services model introduces the concept of a service version, a
finer-grained service notion used to subdivide service instances by
versions (`v1`, `v2`) or environment (`staging`, `prod`). Istio routing
rules can refer to the service versions, to provide additional control over
traffic between services.

The Istio model of services in the application is independent of the
underlying platform (Kubernetes, Mesos, CloudFoundry, etc.). Platform
specific adapters are responsible for populating the internal model
representation with various fields, from the metadata found in the
platform. The following sections describe various aspects of the abstract
service model used by Istio.

## Service

Service is a unit of an application with a unique name that other services
use to refer to the functionality being called. Each service is referred to
using a fully qualified domain name (FQDN) and one or more ports where the
service is listening for connections. Each service has one or more
instances, i.e., actual manifestations of the service.  Instances represent
entities such as containers, pods (kubernetes/mesos), VMs, etc.

## Multiple versions of a service

In a continuous deployment scenario, for a given service, there can be
multiple sets of instances running potentially different variants of the
application binary. These variants are not necessarily different API
versions. They could be iterative changes to the same service, deployed in
different environments (prod, staging, dev, etc.). Common scenarios where
this occurs include A/B testing, canary rollouts, etc.

**Tags** Each version of a service can be differentiated by a unique set of
tags associated with the version. Tags are simple key value pairs
assigned to the instances of a particular service version, i.e., all
instances of same version must have same tag.

## Populating the abstract service model

Where possible, Istio leverages the service registration and discovery
mechanism provided by the underlying platform to populate its abstract
service model. Most container platforms come built in with a service
registry (e.g., kubernetes, mesos) where a pod specification can contain
all the version related tags. Upon launching the pod, the platform
automatically registers the pod with the registry along with the tags.  In
other platforms, a dedicated service registration agent might be needed to
automatically register the service with a service registration/discovery
solution like Consul, etc.

At the moment, Istio integrates readily with Kubernetes service registry
and automatically discovers various services, their pods etc., and
groups the pods into unique sets -- each set representing a service
version. In future, Istio will add support for pulling in similar
information from Mesos registry and *potentially* other registries.

## Communication between services

Clients of a service have no knowledge of different versions of the
service. They can continue to access the services using the hostname/IP
address of the service. Istio Proxy intercepts and forwards all
requests/responses between the client and the service.

The actual choice of the service version is determined dynamically by the
Istio Proxy based on the routing rules set forth by the operator. This
model enables the enables the application code to decouple itself from the
evolution of its dependent services, while providing other benefits as well
(see mixer). Routing rules allow the proxy to select a version based on
criterion such as (headers, url, etc.), tags associated with
source/destination and/or by weights assigned to each version.

Note that Istio does not provide a DNS. Applications can try to resolve the
FQDN using the DNS service present in the underlying platform (kube-dns,
mesos-dns, etc.).
