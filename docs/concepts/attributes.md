---
title: Attributes
headline: Attributes
sidenav: doc-side-concepts-nav.html
bodyclass: docs
layout: docs
type: markdown
---
{% capture overview %}
The page describes Istio attributes, what they are and how they are used.
{% endcapture %}

{% capture body %}

## Background

Istio uses *attributes* to control the runtime behavior of services running in the mesh. Attributes are named and typed pieces of metadata
describing ingress and egress traffic and the environment this traffic occurs in. An Istio attribute carries a specific piece
of information such as the error code of an API request, the latency of an API request, or the
original IP address of a TCP connection. Here are a few examples of attributes:

	request.path: xyz/abc
	request.size: 234
	request.time: 12:34:56.789 04/17/2017
	source.ip: 192.168.0.1
	target.service: example

A given Istio deployment has a fixed vocabulary of attributes that it understands. The specific vocabulary is
determined by the set of attribute producers being used in the deployment. The primary attribute producer in Istio
is the proxy, although specialized mixer adapters and services can also introduce attributes.

The common baseline set of attributes available in most Istio deployments is defined
[here]({{site.baseurl}}/docs/reference/attributes.html). 

{% endcapture %}

{% include templates/concept.md %}
