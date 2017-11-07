# Istio

[![CircleCI](https://circleci.com/gh/istio/istio.svg?style=svg)](https://circleci.com/gh/istio/istio)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/istio)](https://goreportcard.com/report/github.com/istio/istio)
[![GoDoc](https://godoc.org/github.com/istio/istio?status.svg)](https://godoc.org/github.com/istio/istio)
[![codecov.io](https://codecov.io/github/istio/istio/coverage.svg?branch=master)](https://codecov.io/github/istio/istio?branch=master)

An open platform to connect, manage, and secure microservices.

- [Introduction](#introduction)
- [Istio authors](#istio-authors)
- [Repositories](#repositories)
- [Issue management](#issue-management)
- [Community and support](#community-and-support)

In addition, here are some other docs you may wish to read:

- [Istio Working Groups](GROUPS.md) - how we partition work in the project
- [Contribution Guidelines](CONTRIBUTING.md) - explains the process for contributing to the Istio code base
- [Reviewing and Merging Pull Requests](REVIEWING.md) - explains the process we use to review code changes
- [Istio Developer's Guide](DEV-GUIDE.md) - explains how to setup and use an Istio development environment
- [Project conventions](DEV-CONVENTIONS.md) - describes the conventions we use within the code base
- [Creating fast and lean code](DEV-PERF.md) - performance-oriented advice and guidelines for the code base

## Introduction

Istio is an open platform for providing a uniform way to integrate
microservices, manage traffic flow across microservices, enforce policies
and aggregate telemetry data. Istio's control plane provides an abstraction
layer over the underlying cluster management platform, such as Kubernetes,
Mesos, etc.

Visit [istio.io](https://istio.io) for in-depth information about using Istio.     

Istio is composed of these components:

* **Envoy** - Sidecar proxies per microservice to handle ingress/egress traffic
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

* **Pilot** - A component responsible for configuring the
  proxies at runtime.

* **CA** - A component responsible for cert issuance and rotation.

* **Broker** - A component implementing the open service broker API for Istio-based services. (Under development)

Istio currently supports Kubernetes, Consul, qne Eureka-based environments. We plan support for additional platforms such as
Cloud Foundry, and Mesos in the  near future.

## Istio authors

Istio is an open source project with an active development community. The project was started
by teams from Google and IBM, in partnership with the Envoy team at Lyft.

## Repositories

The Istio project is divided across a few GitHub repositories.

- [istio/istio](README.md). This is the main repo that you are
currently looking at. It hosts Istio's core components and also
the sample programs and the various documents that govern the Istio open source
project. It includes:
  - [security](security/). This directory contains security related code,
including CA (Cert Authority), node agent, etc.
  - [pilot](pilot/). This directory
contains platform-specific code to populate the
[abstract service model](https://istio.io/docs/concepts/traffic-management/overview.html), dynamically reconfigure the proxies
when the application topology changes, as well as translate
[routing rules](https://istio.io/docs/reference/config/traffic-rules/routing-rules.html) into proxy specific configuration.  The
[_istioctl_](https://istio.io/docs/reference/commands/istioctl.html) command line utility is also available in
this directory.
  - [mixer](mixer/). This directory
contains code to enforce various policies for traffic passing through the
proxies, and collect telemetry data from proxies and services. There
are plugins for interfacing with various cloud platforms, policy
management services, and monitoring services.
  - [broker](broker/). This directory
contains code for Istio's implementation of the Open Service Broker API.

- [istio/api](https://github.com/istio/api). This repository defines
component-level APIs and common configuration formats for the Istio platform.

- [istio/mixerclient](https://github.com/istio/mixerclient). Client libraries
for the mixer API.

- [istio/proxy](https://github.com/istio/proxy). The Istio proxy contains
extensions to the [Envoy proxy](https://github.com/lyft/envoy) (in the form of
Envoy filters), that allow the proxy to delegate policy enforcement
decisions to the mixer.

## Issue management

We use GitHub combined with ZenHub to track all of our bugs and feature requests. Each issue we track has a variety of metadata:

- **Epic**. An epic represents a feature area for Istio as a whole. Epics are fairly broad in scope and are basically product-level things.
Each issue is ultimately part of an epic.

- **Milestone**. Each issue is assigned a milestone. This is 0.1, 0.2, 0.3, or 'Nebulous Future'. The milestone indicates when we
think the issue should get addressed.

- **Priority/Pipeline**. Each issue has a priority which is represented by the Pipeline field within GitHub. Priority can be one of
P0, P1, P2, or >P2. The priority indicates how important it is to address the issue within the milestone. P0 says that the
milestone cannot be considered achieved if the issue isn't resolved.

We don't annotate issues with Releases; Milestones are used instead. We don't use GitHub projects at all, that
support is disabled for our organization.

## Community and support

There are several [communication channels](https://istio.io/community/) available to get
support for Istio or to participate in its evolution.
