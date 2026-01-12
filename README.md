# Istio

[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/1395/badge)](https://bestpractices.coreinfrastructure.org/projects/1395)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/istio)](https://goreportcard.com/report/github.com/istio/istio)
[![GoDoc](https://godoc.org/istio.io/istio?status.svg)](https://godoc.org/istio.io/istio)

<a href="https://istio.io/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/projects/istio/icon/color/istio-icon-color.svg">
      <source media="(prefers-color-scheme: light)" srcset="https://github.com/istio/istio/raw/master/logo/istio-bluelogo-whitebackground-unframed.svg">
      <img title="Istio" height="100" width="100" alt="Istio logo" src="https://github.com/istio/istio/raw/master/logo/istio-bluelogo-whitebackground-unframed.svg">
    </picture>
</a>

---

Istio is an open source service mesh that layers transparently onto existing distributed applications. Istio’s powerful features provide a uniform and more efficient way to secure, connect, and monitor services. Istio is the path to load balancing, service-to-service authentication, and monitoring – with few or no service code changes.

- For in-depth information about how to use Istio, visit [istio.io](https://istio.io)
- To ask questions and get assistance from our community, visit [GitHub Discussions](https://github.com/istio/istio/discussions)
- To learn how to participate in our overall community, visit [our community page](https://istio.io/about/community)

In this README:

- [Introduction](#introduction)
- [Repositories](#repositories)
- [Issue management](#issue-management)

In addition, here are some other documents you may wish to read:

- [Istio Community](https://github.com/istio/community#istio-community) - describes how to get involved and contribute to the Istio project
- [Istio Developer's Guide](https://github.com/istio/istio/wiki/Preparing-for-Development) - explains how to set up and use an Istio development environment
- [Project Conventions](https://github.com/istio/istio/wiki/Development-Conventions) - describes the conventions we use within the code base
- [Creating Fast and Lean Code](https://github.com/istio/istio/wiki/Writing-Fast-and-Lean-Code) - performance-oriented advice and guidelines for the code base

You'll find many other useful documents on our [Wiki](https://github.com/istio/istio/wiki).

## Introduction

[Istio](https://istio.io/latest/docs/concepts/what-is-istio/) is an open platform for providing a uniform way to [integrate
microservices](https://istio.io/latest/docs/examples/microservices-istio/), manage [traffic flow](https://istio.io/latest/docs/concepts/traffic-management/) across microservices, enforce policies
and aggregate telemetry data. Istio's control plane provides an abstraction
layer over the underlying cluster management platform, such as Kubernetes.

Istio is composed of these components:

- **Envoy** - Sidecar proxies per microservice to handle ingress/egress traffic
   between services in the cluster and from a service to external
   services. The proxies form a _secure microservice mesh_ providing a rich
   set of functions like discovery, rich layer-7 routing, circuit breakers,
   policy enforcement and telemetry recording/reporting
   functions.

  > Note: The service mesh is not an overlay network. It
  > simplifies and enhances how microservices in an application talk to each
  > other over the network provided by the underlying platform.

* **Ztunnel** - A lightweight data plane proxy written in Rust,
    used in Ambient mesh mode to provide secure connectivity and observability for workloads without sidecar proxies.

- **Istiod** - The Istio control plane. It provides service discovery, configuration and certificate management.

## Repositories

The Istio project is divided across a few GitHub repositories:

- [istio/api](https://github.com/istio/api). This repository defines
component-level APIs and common configuration formats for the Istio platform.

- [istio/community](https://github.com/istio/community). This repository contains
information on the Istio community, including the various documents that govern
the Istio open source project.

- [istio/istio](README.md). This is the main code repository. It hosts Istio's
core components, install artifacts, and sample programs. It includes:

    - [istioctl](istioctl/). This directory contains code for the
[_istioctl_](https://istio.io/latest/docs/reference/commands/istioctl/) command line utility.

    - [pilot](pilot/). This directory
contains platform-specific code to populate the
[abstract service model](https://istio.io/docs/concepts/traffic-management/#pilot), dynamically reconfigure the proxies
when the application topology changes, as well as translate
[routing rules](https://istio.io/latest/docs/reference/config/networking/) into proxy specific configuration.

    - [security](security/). This directory contains [security](https://istio.io/latest/docs/concepts/security/) related code.

- [istio/proxy](https://github.com/istio/proxy). The Istio proxy contains
extensions to the [Envoy proxy](https://github.com/envoyproxy/envoy) (in the form of
Envoy filters) that support authentication, authorization, and telemetry collection.

- [istio/ztunnel](https://github.com/istio/ztunnel). The repository contains the Rust implementation of the ztunnel
component of Ambient mesh.

- [istio/client-go](https://github.com/istio/client-go). This repository defines
  auto-generated Kubernetes clients for interacting with Istio resources programmatically.

> [!NOTE]
> Only the `istio/api` and `istio/client-go` repositories expose stable interfaces intended for direct usage as libraries.

## Issue management

We use GitHub to track all of our bugs and feature requests. Each issue we track has a variety of metadata:

- **Epic**. An epic represents a feature area for Istio as a whole. Epics are fairly broad in scope and are basically product-level things.
Each issue is ultimately part of an epic.

- **Milestone**. Each issue is assigned a milestone. This is 0.1, 0.2, ..., or 'Nebulous Future'. The milestone indicates when we
think the issue should get addressed.

- **Priority**. Each issue has a priority which is represented by the column in the [Prioritization](https://github.com/orgs/istio/projects/6) project. Priority can be one of
P0, P1, P2, or >P2. The priority indicates how important it is to address the issue within the milestone. P0 says that the
milestone cannot be considered achieved if the issue isn't resolved.

---

<div align="center">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
      <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/cncf/artwork/master/other/cncf/horizontal/color/cncf-color.svg">
      <img width="300" alt="Cloud Native Computing Foundation logo" src="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
    </picture>
    <p>Istio is a <a href="https://cncf.io">Cloud Native Computing Foundation</a> project.</p>
</div>
