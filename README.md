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
   services. The proxies form a _secure Layer-7 microservice mesh_
   providing a rich set of functions like discovery, rich layer-7 routing,
   circuit breakers, policy enforcement and telemetry recording/reporting
   functions.

* **Mixer** - Central component that co-ordinates with various proxies to
   enforce policies such as ACLs, rate limits, authentication, request
   tracing and metrics collection.

* **Manager** - A configuration manager responsible for configuring the
  proxies and the mixer at runtime.

A high-level overview of various components in Istio is available
[here](doc/overview.md). In terms of platforms, Istio currently supports
Kubernetes. We plan to add support for additional platforms in the near
future. See the [getting started](doc/getting-started.md) tutorial for more
information on using Istio in your Kubernetes deployments.

## Repositories

The Istio project is divided across multiple GitHub repositories. Each
repository contains information about how to build and test it.

- [istio/api](https://github.com/istio/api). This repository defines
component-level APIs and common configuration formats for the Istio platform.

- [istio/istio](README.md). This is the main Istio repo (the one you are
currently looking at). It hosts hosts the high-level documentation for the
project, along with [tutorials](doc/getting-started.md) and two demo
applications: a [basic echo app](demos/apps/simple_echo_app) and a slightly
more advanced [polyglot application](demos/apps/bookinfo).

- [istio/manager](https://github.com/istio/manager). The Istio manager is 
used to configure Istio.  It propagates configuration to the other components 
of the system, including the Istio mixer and the Istio proxies.  The [_istioctl_](doc/istioctl.md)
command line utility is also available in this repository.

- [istio/mixer](https://github.com/istio/mixer). The Istio mixer is the nexus of
the Istio service mesh. The proxy delegates policy decisions to the mixer.  
Proxies and Istio-managed services direct telemetry data to the
mixer. The mixer includes a flexible plugin model enabling it to interface to
a variety of host environments and configured backends, abstracting the 
proxy and Istio-managed services from these details.

- [istio/mixerclient](https://github.com/istio/mixerclient). Client libraries
for the mixer API.

- [istio/proxy](https://github.com/istio/proxy). The Istio proxy is a
microservice proxy that can be used on the client and server side, and forms a
microservice mesh. 

## Contributing to the project

See the [contribution guidelines](CONTRIBUTING.md) for information on how to
participate in the Istio project by submitting pull requests or issues. 

## Community and support

There are several communication channels available:

- [Mailing List](https://groups.google.com/forum/#!forum/istio-dev)
- [Slack](https://istio-dev.slack.com)

and of course use GitHub issues to report bugs or problems to the team:
 
- [Overall Istio Issues](https://github.com/istio/istio/issues)
- [Manager Issues](https://github.com/istio/manager/issues)
- [Mixer Issues](https://github.com/istio/mixer/issues)
- [Proxy Issues](https://github.com/istio/proxy/issues)
