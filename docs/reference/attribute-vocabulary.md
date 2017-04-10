---
title: Attribute Vocabulary
headline: 'Attribute Vocabulary'
sidenav: doc-side-reference-nav.html
bodyclass: docs
layout: docs
type: markdown
---

Attributes are a central concept used throughout Istio. You can find a description of what attributes are
and what they are used for [here]({{site.baseurl}}/docs/concepts/attributes.md).

A given Istio deployment has a fixed vocabulary of attributes that it understands. The specific vocabulary is
determined by the set of attribute producers being used in the deployment. The primary attribute producer in Istio
is the proxy, although the mixer and services can also introduce attributes.

## Standard Istio attribute vocabulary

The table below shows the set of canonical attributes and their respective types. Most Istio
deployments will have agents (proxy or mixer adapters) that produce these attributes.

| Name | Type | Description | Kubernetes Example |
|------|------|-------------|--------------------|
| source.ip | ip_address | The IP address of the client that sent the request. | 10.0.0.117 |
| source.port | int64 | The port on the client that sent the request. | 9284 |
| source.name | string | The fully qualified name of the application that sent the request. | redis-master.namespace.deps.cluster.local |
| source.uid | string | A unique identifier for the particular instance of the application that sent the request. | redis-master-2353460263-1ecey.namespace.pods.cluster.local |
| source.namespace | string | The namespace of the sender of the request. | namespace.cluster.local |
| source.labels | map | A map of key-value pairs attached to the source. | |
| source.user | string | The user running the target application. | service-account@namespace.cluster.local |
| target.ip | ip_address | The IP address the request was sent to. | 10.0.0.104 |
| target.port | int64 | The port the request was sent to. | 8080 |
| target.service | dns_name | The hostname that was the target of the request. | my-svc.my-namespace.svc.cluster.local |
| target.name | string | The fully qualified name of the application that was the target of the request. | |
| target.uid | string | A unique identifier for the particular instance of the application that received the request. | |
| target.namespace | string | The namespace of the receiver of the request. | |
| target.labels | map | A map of key-value pairs attached to the target. | |
| target.user | string | The user running the target application. | service-account@namespace.cluster.local |
| origin.ip | ip_address | The IP address of the originating application | |
| origin.name | string | The fully qualified name of the originating application | |
| origin.uid | string | A unique identifier for the particular instance of the application that originated the request. This must be unique within the cluster. | for in cluster pod-id gives sufficient to ascertain all other attributes needed by the policy. |
| origin.namespace | string | The namespace of the originator of the request. | |
| origin.labels | map | A map of key-value pairs attached to the origin. | |
| origin.user | string | The user running the originating application. | |
| request.headers | map | A map of HTTP headers attached to the request. | |
| request.id | string | A unique ID for the request, which can be propagated to downstream systems. This should be a guid or a psuedo-guid with a low probability of collision in a temporal window measured in days or weeks. | |
| request.path | string | The HTTP URL path including query string | |
| request.host | string | The HTTP Host header. | |
| request.method | string | The HTTP method. | |
| request.reason | string | The system parameter for auditing reason. It is required for cloud audit logging and GIN logging | |
| request.referer | string | The HTTP referer header. | |
| request.scheme | string | URI Scheme of the request | |
| request.size | int64 | Size of the request in bytes. For HTTP requests this is equivalent to the Content-Length header. | |
| request.time | timestamp | The timestamp when the target receives the request. This should be equivalent to Firebase "now". | |
| request.user-agent | string | The HTTP User-Agent header. | |
| response.headers | map | A map of HTTP headers attached to the response. | |
| response.size | int64 | Size of the response body in bytes | |
| response.time | timestamp | The timestamp when the target produced the response. | |
| response.duration | duration | The amount of time the response took to generate. | |
| response.code | int64 | The response's HTTP status code | |
