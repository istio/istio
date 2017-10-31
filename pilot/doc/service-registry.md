# Istio services model

This file describes the abstract model of services (and their instances)
as represented in Istio. This model is independent of the underlying
platform (Kubernetes, Mesos, etc.). Platform specific adapters found
under [platform](../platform) populate the model object with various fields, from the
metadata found in the platform.  The platform independent proxy code
under [proxy](../proxy) uses the representation in the model to generate the
configuration files for the Layer 7 proxy sidecar. The proxy code is
specific to individual proxy implementations

## Glossary & concepts

Service is a unit of an application with a unique name that other
services use to refer to the functionality being called. Service
instances are pods/VMs/containers that implement the service.

There are multiple versions of a service - In a continuous deployment
scenario, for a given service, there can be multiple sets of instances
running potentially different variants of the application binary. These
variants are not necessarily different API versions. They could be
iterative changes to the same service, deployed in different
environments (prod, staging, dev, etc.). Common scenarios where this
occurs include A/B testing, canary rollouts, etc.

### 1. Services

Each service has a fully qualified domain name (FQDN) and one or more
ports where the service is listening for connections. *Optionally*, a
service can have a single load balancer/virtual IP address associated
with it, such that the DNS queries for the FQDN resolves to the virtual
IP address (a load balancer IP).

E.g., in kubernetes, a service foo is associated with
`foo.default.svc.cluster.local hostname`, has a virtual IP of 10.0.1.1 and
listens on ports 80, 8080

### 2. Instances

Each service has one or more instances, i.e., actual
manifestations of the service.  Instances represent entities such as
containers, pods (kubernetes/mesos), VMs, etc.  For example, imagine
provisioning a nodeJs backend service called catalog with hostname
(catalogservice.mystore.com), running on port 8080, with 10 VMs hosting
the service.

Note that in the example above, the VMs in the backend do not
necessarily have to expose the service on the same port. Depending on
the networking setup, the instances could be NAT-ed (e.g., mesos) or be
running on an overlay network.  They could be hosting the service on any
random port as long as the load balancer knows how to forward the
connection to the right port.

For e.g., A call to http://catalogservice.mystore.com:8080/getCatalog
would resolve to a load balancer IP 10.0.0.1:8080, the load balancer
would forward the connection to one of the 10 backend VMs, e.g.,
172.16.0.1:55446 or 172.16.0.2:22425, 172.16.0.3:35345, etc.

Network Endpoint: The network IP address and port associated with each
instance (e.g., 172.16.0.1:55446 in the above example) is called the
NetworkEndpoint. Calls to the service's load balancer (virtual) IP
10.0.0.1:8080 or the hostname catalog.mystore.com:8080 will end up being
routed to one of the actual network endpoints.

Services do not necessarily have to have a load balancer IP. They can
have a simple DNS SRV based system, such that the DNS srv call to
resolve catalog.mystore.com:8080 resolves to all 10 backend IPs
(172.16.0.1:55446, 172.16.0.2:22425,...).


### 3. Service versions

Each version of a service can be differentiated by a unique set of
labels associated with the version. Labels are simple key value pairs
assigned to the instances of a particular service version, i.e., all
instances of same version must have same tag. For example, lets say
catalog.mystore.com has 2 versions v1 and v2.

Lets say v1 has labels gitCommit=aeiou234, region=us-east and v2 has labels
name=kittyCat,region=us-east. And lets say instances 172.16.0.1
.. 171.16.0.5 run version v1 of the service.

These instances should register themselves with a service registry,
using the labels gitCommit=aeiou234, region=us-east, while instances
172.16.0.6 .. 172.16.0.10 should register themselves with the service
registry using the labels name=kittyCat,region=us-east

Istio expects that the underlying platform to provide a service registry
and service discovery mechanism. Most container platforms come built in
with a service registry (e.g., kubernetes, mesos) where a pod
specification can contain all the version related labels. Upon launching
the pod, the platform automatically registers the pod with the registry
along with the labels.  In other platforms, a dedicated service
registration agent might be needed to automatically register the service
with a service registration/discovery solution like Consul, etc.

At the moment, Istio integrates readily with Kubernetes service registry
and automatically discovers various services, their pods etc., and
groups the pods into unique sets -- each set representing a service
version. In future, Istio will add support for pulling in similar
information from Mesos registry and *potentially* other registries.

### 4. Service version labels

When listing the various instances of a service, the labels partition
the set of instances into disjoint subsets.  E.g., grouping pods by labels
"gitCommit=aeiou234,region=us-east", will give all instances of v1 of
service catalog.mystore.com

In the absence of a multiple versions, each service has a
default version that consists of all its instances. For e.g., if pods
under catalog.mystore.com did not have any labels associated with them,
Istio would consider catalog.mystore.com as a service with just one
default version, consisting of 10 VMs with IPs 172.16.0.1 .. 172.16.0.10

### 5. Routing

Applications have no knowledge of different versions of the
service. They can continue to access the services using the hostname/IP
address of the service, while Istio will take care of routing the
connection/request to the appropriate version based on the routing rules
set up by the admin. This model enables the application code
to decouple itself from the evolution of its dependent services, while
providing other benefits as well (see mixer).

Note that Istio does not provide a DNS. Applications can try to resolve
the FQDN using the DNS service present in the underlying platform
(kube-dns, mesos-dns, etc.).  In certain platforms such as kubnernetes,
the DNS name resolves to the service's load balancer (virtual) IP
address, while in other platforms, the DNS names might resolve to one or
more instance IP addresses (e.g., in mesos-dns, via DNS srv). Neither
scenario has no bearing on the application code.

The Istio proxy sidecar intercepts and forwards all requests/responses
between the application and the service.  The actual choice of the
service version is determined dynamically by the proxy sidecar process
(e.g., Envoy, nginx) based on the routing rules set forth by the
administrator. There are layer7 (http) and layer4 routing rules (see the
proxy config proto definition for more details).

Routing rules allow the proxy to select a version based on criterion
such as (headers, url, etc.), labels associated with source/destination
and/or by weights assigned to each version.

