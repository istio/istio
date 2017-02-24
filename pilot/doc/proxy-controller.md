# Proxy controller

Istio Manager controls the mesh of Istio Proxies by propagating service registry information and routing rules to the destination proxies. Currently, Istio Proxy is based on [Envoy](github.com/lyft/envoy), and the controller for Envoy consists of two parts:

- proxy agent, a supervisor script that generates Envoy configuration from the abstract model of services and rules, and triggers a proxy restart
- discovery service, an Envoy Discovery Service that publishes information about service - instance IP mappings.

## Proxy injection

To ensure that all traffic is trapped by Istio Proxy, Istio Manager relies on iptables rules. The service instances communicate using regular HTTP headers, but all requests are captured by Istio Proxy and re-routed based on the request metadata and Istio routing rules.

The details for proxy injection are [here](proxy-injection.md). Since all network traffic is captured, external services require special external service representation in the service model. 

## Proxy agent

Proxy agent is a simple agent whose primary duty is to subscribe to changes in the mesh topology and configuration store, and reconfigure proxy. As parts of Envoy configuration become available through discovery services, we are gradually delegating configuration generation to the discovery service.

## Discovery service

Discovery service publishes service topology and routing information to all proxies in the mesh. Each proxy carries an identity (pod name, in case of Kubernetes sidecar deployment).

## Routing rules

Routing rules follow a proto3 schema [here](../model/proxy/alphav1/config/cfg.proto). Examples are available in the [integration tests](../test/integration).

## Ingress and egress

TBD
