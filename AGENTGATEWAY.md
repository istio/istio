# Agentgateway Support in Istio

Istio has **experimental** support for running the [agentgateway](https://github.com/agentgateway/agentgateway/) proxy as a dataplane. Agentgateway is a high-performance, lightweight proxy designed for cloud-native AI applications. Its primary control plane is the [kgateway](https://github.com/kgateway-dev/kgateway/) CNCF sandbox project, and the two together are fully conformant with the Gateway API. This document outlines the contract between the Istio and agentgateway projects and sets explicit milestones and advancement criteria for continued integration.

## Scope of the Integration

Agentgateway has a variety of features that kgateway (its primary control plane) exposes through numerous custom resources. However, Istio's integration with agentgateway will expose these features __almost exclusively through Gateway API__ and related subprojects and working groups (e.g. [kube-agentic-networking](https://github.com/kubernetes-sigs/kube-agentic-networking) and [wg-ai-gateway](https://github.com/kubernetes-sigs/wg-ai-gateway/)). This means that Istio users will be able to configure and manage agentgateway proxies using Gateway API resources, while other custom resources specific to kgateway will not be supported in this integration. This is a purposeful decision to maintain a clear and focused integration between Istio and agentgateway, leveraging the standardized Gateway API for configuration and management. We will start with the following exceptions and the TOC can approve new exceptions as needed:
- `AuthorizationPolicy`
- `RequestAuthentication`

Since AI capabilities are the biggest gap in Istio today, we will start this integration with support for ingress and egress workloads. Once there is sufficient feedback and stability in these scenarios (over a time period of **2 releases**), we will begin expanding support to ambient mode as a waypoint. Sidecar mode is NOT in scope for this integration at this time due to concerns with the sidecar model in general.

## Governance and Advancement Criteria

While agentgateway is not a CNCF project, it has been donated to the [Linux Foundation (LF)](https://www.linuxfoundation.org/press/linux-foundation-welcomes-agentgateway-project-to-accelerate-ai-agent-adoption-while-maintaining-security-observability-and-governance), providing similar guarantees around open governance and community collaboration. That being said, the LF does not have the same project graduation model as CNCF, so we will define our own advancement criteria for this integration. This is to ensure that users and maintainers alike have a clear understanding of the maturity and stability of the integration over time. Here, we outline the beta and GA gates for the Istio and agentgateway integration:

## Beta Criteria

- Consistently passing experimental Gateway API conformance tests for ingress and egress scenarios.
- Clear documentation and examples for setting up and using agentgateway with Istio.
- Istio integration tests covering any gaps where Gateway API conformance tests do not exist (e.g. implementation specific behavior).
- Agentgateway has maintainers from at least 2 different organizations.
- Agentgateway has a defined security policy and a process for reporting and addressing vulnerabilities.
- Positive feedback from early adopters and community members using the integration in real-world scenarios (including and especially AI).
- Performance testing akin to [gateway-api-bench](https://github.com/howardjohn/gateway-api-bench/blob/v2/README-v2.md) demonstrating clear performance and scale improvements over the Envoy-based Gateway implementation in Istio.

## GA Criteria

- All beta criteria are met and have been stable for at least 2 Istio releases.
- Comprehensive Gateway API conformance tests passed for all supported scenarios (ingress, egress, and waypoint).
- Agentgateway has maintainers from at least 3 different organizations.
- A well-defined roadmap for future enhancements and features in the integration.
