# Proxy controller

Istio Pilot controls the mesh of Istio Proxies by propagating service registry information and routing rules to the destination proxies. Currently, Istio Proxy is based on [Envoy](https://github.com/lyft/envoy), and the controller for Envoy consists of two parts:

- proxy agent, a supervisor script that generates Envoy configuration from the abstract model of services and rules, and triggers a proxy restart
- discovery services, implementing Envoy Discovery Service APIs, that publish information for Envoy proxies to consume.

## Proxy injection

To ensure that all traffic is trapped by Istio Proxy, Istio Pilot relies on iptables rules. The service instances communicate using regular HTTP headers, but all requests are captured by Istio Proxy and re-routed based on the request metadata and Istio routing rules.

The details for proxy injection are [here](proxy-injection.md). Since all network traffic is captured, external services require special external service representation in the service model. 

## Proxy agent

Proxy agent is a simple agent whose primary duty is to subscribe to changes in the mesh topology and configuration store, and reconfigure proxy. As more and more parts of Envoy configuration become available through discovery services, we are gradually delegating configuration generation to the discovery services. For example, TCP proxy configuration is mostly configured through the local proxy agent since Envoy has not implemented support for the route discovery for the `tcp_proxy` filter.

## Discovery service

Discovery service publishes service topology and routing information to all proxies in the mesh. Each proxy carries an identity (pod name and IP address, in case of Kubernetes sidecar deployment). Envoy uses this identity to construct a request to the discovery service. The discovery service computes the set of service instances running at the proxy address from the service registry, and creates Envoy configuration adapted to the proxy making the request. 

There are three types of discovery services exposed by Istio Pilot:

- SDS is the service discovery that is responsible for listing a set of `ip:port` pairs for a cluster;
- CDS is the cluster discovery that is responsible for listing all Envoy clusters;
- RDS is the route discovery that is responsible for listing HTTP routes; the proxy identity is important for applying route rules with source service conditions.

## Routing rules

Routing rules are defined by Istio API [proto schema](https://github.com/istio/api/blob/master/proxy/v1/config/route_rule.proto). Examples are available in the [integration tests](../test/integration).

## Ingress and egress

TBD
