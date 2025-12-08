# Agentgateway Support in Istio

Istio is pursuing **experimental** support for running the [agentgateway](https://github.com/agentgateway/agentgateway/) proxy as a dataplane. More specifically, this integration will occur in two phases:
Phase 1: Ingress and Gateway API style egress support
Phase 2: Ambient waypoint mode support (incl. ambient-style egress)
Sidecar mode is out of scope at this time.

Agentgateway is a high-performance, lightweight proxy designed for cloud-native AI applications. Its primary control plane is the [kgateway](https://github.com/kgateway-dev/kgateway/) CNCF sandbox project, and the two together are fully conformant with the Gateway API. This document outlines the contract between the Istio and agentgateway projects and sets explicit milestones and advancement criteria for continued integration.

## Scope of the Integration

Agentgateway has a variety of features that kgateway (its primary control plane) exposes through numerous custom resources. However, Istio's integration with agentgateway will expose these features __almost exclusively through Gateway API__ and related subprojects and working groups (e.g. [kube-agentic-networking](https://github.com/kubernetes-sigs/kube-agentic-networking) and [wg-ai-gateway](https://github.com/kubernetes-sigs/wg-ai-gateway/)). This means that Istio users will be able to configure and manage agentgateway proxies using Gateway API resources, while other custom resources specific to kgateway will not be supported in this integration. This is a purposeful decision to maintain a clear and focused integration between Istio and agentgateway, leveraging the standardized Gateway API for configuration and management. The Istio TOC can approve exceptions for specific resources on a case-by-case basis if there is a compelling use case and no viable alternative using Gateway API.

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
- Full documentation matrix for all features supported/not supported when using envoy vs. agentgateway.

## User Experience (UX) Considerations

While agentgateway provides many of the same features that Envoy does, it is NOT meant to be a drop-in replacement. We expect that there will be behavioral differences between the two, some of which will be intentional. We believe this is acceptable because, again, this integration is not meant to "phase-out" Envoy but rather to provide an alternative option for users who e.g. need AI features or lower resource utilization for basic use-cases (exposed by Gateway API).

At a high level, the same Gateway API configuration should work ~essentially the same between the Envoy and agentgateway implementations. The main differences will be in the proxy-specific settings and representations, which may include:
- Buffering and header limits
- Connection pooling
- Load balancing algorithms
- Exact metric names and formats
- Error handling and logging
- etc...

On telemetry specifically, most of the tablestakes observability provided by Envoy exists in agentgateway (short of implementation-details like filter state or error codes like "UX"). Agentgateway generally exposes its functionality via OpenTelemetry as the standard interface vs. one-off integrations with a variety of providers. Any functional gaps will be documented and addressed if possible.

### Extensibility

The biggest difference between agentgateway and Envoy w.r.t features is around extensibility. Envoy has a variety of ways to add custom functionality into the proxy, including:
- Compiling in custom filters written in C++
- Lua scripting
- WASM modules (following the proxy-wasm ABI)
- Dynamic modules
- CEL expressions in various places

As a rule, agentgateway does not currently plan to allow arbitrary logic to execute within the proxy itself. This is both to limit the complexity of the proxy and to ensure the security and performance posture of the project remains manageable, especially early on. If/when agentgateway does support more extensibility options (akin to what we support in Istio), we will revisit if/how we can support them via this integration.



