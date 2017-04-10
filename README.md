# Istio
A service mesh for polyglot microservices.

- [Introduction](#introduction)
- [Repositories](#repositories)
- [Contributing to the project](#contributing-to-the-project)
- [Community and support](#community-and-support)

## Introduction

Istio is an open platform for providing a uniform way to integrate
microservices, manage traffic flow across microservices, enforce policies
and aggregate telemetry data. Istio's control plane provides an abstraction
layer over the underlying cluster management platform, such as Kubernetes,
Mesos, etc.

Istio is composed of three main components:

* **Proxy** - Sidecars per microservice to handle ingress/egress traffic
   between services in the cluster and from a service to external
   services. The proxies form a _secure microservice mesh_ providing a rich
   set of functions like discovery, rich layer-7 routing, circuit breakers,
   policy enforcement and telemetry recording/reporting
   functions.

  >  Note: The service mesh is not an overlay network. It
  >  simplifies and enhances how microservices in an application talk to each
  >  other over the network provided by the underlying platform.

* **Mixer** - Central component that is leveraged by the proxies and microservices
   to enforce policies such as ACLs, rate limits, quotas, authentication, request
   tracing and telemetry collection.

* **Manager** - A component responsible for configuring the
  proxies and the mixer at runtime.

Istio currently only supports the Kubernetes
platform, although we plan support for additional platforms such as
CloudFoundry, and Mesos in the near future.

You can learn all about Istio by visiting [istio.io](https://istio.io).     

## Repositories

The Istio project is divided across multiple GitHub repositories. Each
repository contains information about how to build and test it.

- [istio/api](https://github.com/istio/api). This repository defines
component-level APIs and common configuration formats for the Istio platform.

- [istio/istio](README.md). This is the repo you are
currently looking at. It hosts the various Istio sample programs
along with the various documents that govern the Istio open source 
project.

- [istio/manager](https://github.com/istio/manager). This repository
contains platform-specific code to populate the
[abstract service model](https://istio.io/docs/concepts/model.html), dynamically reconfigure the proxies
when the application topology changes, as well as translate
[routing rules](https://istio.io/docs/reference/rule-dsl.html) into proxy specific configuration.  The
[_istioctl_](https://istio.io/docs/reference/istioctl.html) command line utility is also available in
this repository.

- [istio/mixer](https://github.com/istio/mixer). This repository 
contains code to enforce various policies for traffic passing through the
proxies, and collect telemetry data from proxies and microservices. There
are plugins for interfacing with various cloud platforms, policy
management services, and monitoring services.

- [istio/mixerclient](https://github.com/istio/mixerclient). Client libraries
for the mixer API.

- [istio/proxy](https://github.com/istio/proxy). The Istio proxy contains
extensions to the [Envoy proxy](https://github.com/lyft/envoy) (in the form of
Envoy filters), that allow the proxy to delegate policy enforcement
decisions to the mixer.

## Contributing to the project

See the [contribution guidelines](CONTRIBUTING.md) for information on how to
participate in the Istio project by submitting pull requests or issues. 

## Community and support

There are several [communication channels](https://istio.io/community) available to get
support for Istio or to participate in its evolution.
