# Istio
A service mesh for polyglot microservices.

- [Introduction](#introduction)
- [Repositories](#repositories)
- [Contributing to the project](#contributing-to-the-project)
- [Community and support](#community-and-support)

## Introduction

Istio is an open source system providing a uniform way to manage and connect microservices.
It is composed of:
*  A proxy handling service-to-service and external-to-service traffic.
*  A mixer supporting access checks, quota allocation and deallocation, monitoring and logging.
*  A manager handling system configuration, discovery, and automation.

The [architectural overview](ARCHITECTURE.md) provides a high-level summary of the design. The
[milestone plan](MILESTONES.md) gives a rough estimate of what we expect to release and when.

## Repositories

The Istio project is divided across multiple GitHub repositories. Each repository contains
information about how to build and test it.

- [istio/api](https://github.com/istio/api). This repository defines component-level APIs and common configuration 
formats for the Istio platform.

- [istio/istio](https://github.com/istio/istio). The main Istio repo which is used to host the high-level documentation
for the project, along with examples & demos.

- [istio/manager](https://github.com/istio/manager). The Istio manager is used to configure Istio and propagate configuration to 
the other components of the system, including the Istio mixer and the Istio 
proxy mesh.

- [istio/mixer](https://github.com/istio/mixer). The Istio mixer is the nexus of the Istio service mesh. The proxy delegates policy decisions to the mixer, 
and both the proxy and Istio-managed services direct all telemetry data to the mixer. The mixer includes a flexible plugin model enabling it
to interface to a variety of host environments and configured backends, abstracting the proxy and Istio-managed services from these details.

- [istio/mixerclient](https://github.com/istio/mixerclient). Client libraries for the mixer API.

- [istio/proxy](https://github.com/istio/proxy). The Istio proxy is a microservice proxy that can be used on the client and 
server side, and forms a microservice mesh. 

## Contributing to the project

See the [contribution guidelines](CONTRIBUTING.md) for information on how to participate in the Istio
project by submitting pull requests or issues. 

## Community and support

There are several communication channels available:

- [Mailing List](https://groups.google.com/forum/#!forum/istio-dev)
- [Slack](https://istio-dev.slack.com)

and of course use GitHub issues to report bugs or problems to the team:
 
- [Overall Istio Issues](https://github.com/istio/istio/issues)
- [Manager Issues](https://github.com/istio/manager/issues)
- [Mixer Issues](https://github.com/istio/mixer/issues)
- [Proxy Issues](https://github.com/istio/proxy/issues)
