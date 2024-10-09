# Istio Integration Tests

This document highlights the different integration test setups and architectural configurations for setting up the tests. It also provides guidelines for adding integration tests to the Istio project, specifically focusing on the differences between adding tests to the various folders under `tests/integration`. It also explains the implications of using the main test setup in each folder.

## Overview

Integration tests in Istio are essential for ensuring that various components work together as expected. Depending on the component you are testing, you may need to add your tests to different folders within the `tests/integration` directory. This document will guide you through the process of adding integration tests and understanding the setup requirements.

## Integration Tests High-Level Architecture

### Pilot Integration Tests

- **Location**: `tests/integration/pilot`
- **Purpose**: Tests related to the Istio Pilot component, which is responsible for configuring the Envoy proxies.
- **Focus**:
  1. Configuration of Envoy proxies by Pilot.
  1. Communication between Pilot and Envoy proxies.
  1. Validation of service discovery.
  1. Testing of traffic management policies (e.g., routing, retries, timeouts).
  1. Validation of load balancing configurations.
  1. Specific `istioctl proxy-config` commands being tested: `bootstrap`, `cluster`, `endpoint`, `listener`, `route`, `all`.
- **Setup**: The main test setup in this folder initializes the Istio control plane and configures the Pilot component.

### Ambient Integration Tests

- **Location**: `tests/integration/ambient`
- **Purpose**: Tests related to the Ambient mode, including components like `ztunnel`.
- **Focus**:
  1. Configuration and communication of Ambient components.
  1. Interaction between `ztunnel` and Ambient components.
  1. Validation of zero-trust security policies.
  1. Testing of ambient traffic management.
  1. Specific `istioctl ztunnel-config` commands being tested: `all`, `services`, `workloads`, `policies`, `certificates`.
- **Setup**: The main test setup in this folder initializes the Istio control plane, `ztunnel`, and other ambient components.

### Telemetry Integration Tests

- **Location**: `tests/integration/telemetry`
- **Purpose**: Tests related to telemetry features, including metrics, logging, and tracing.
- **Focus**:
  1. Collection and processing of telemetry data.
  1. Interaction between telemetry components and Istio control plane.
  1. Validation of metrics collection and reporting.
  1. Testing of logging configurations and log collection.
  1. Validation of tracing and distributed tracing setups.
- **Setup**: The main test setup in this folder initializes the Istio control plane with telemetry configurations.

### Helm Integration Tests

- **Location**: `tests/integration/helm`
- **Purpose**: Tests related to Helm charts and their deployment.
- **Focus**:
  1. Deployment of Istio using Helm charts.
  1. Verification of Helm chart configurations.
  1. Testing of Helm chart upgrades and rollbacks.
  1. Validation of custom Helm values and overrides.
  1. Ensuring compatibility with different Kubernetes versions.
- **Setup**: The main test setup in this folder initializes the Istio control plane using Helm charts.

### Security Integration Tests

- **Location**: `tests/integration/security`
- **Purpose**: Tests related to the security features and components of Istio, such as authentication and authorization mechanisms.
- **Focus**:
  1. Authentication and authorization mechanisms.
  1. Interaction between security components and Istio control plane.
  1. Validation of mutual TLS (mTLS) configurations.
  1. Testing of JWT token validation and RBAC policies.
  1. Validation of certificate management and rotation.
- **Setup**: The main test setup in this folder initializes the Istio control plane with security configurations.

## Adding a New Integration Test

For detailed instructions on adding a new integration test, please refer to the [Integration Tests README](https://github.com/istio/istio/blob/master/tests/integration/README.md).

## Architectural Considerations

### Pilot Tests

- **Component Focus**: Primarily focuses on the Pilot component and its interactions with Envoy proxies.
- **Setup Requirements**: Requires the Istio control plane to be initialized with Pilot.

### Ambient Tests

- **Component Focus**: Primarily focuses on Ambient mode components, including `ztunnel`.
- **Setup Requirements**: Requires the Istio control plane to be initialized with `ztunnel` and other ambient components.

### Telemetry Tests

- **Component Focus**: Primarily focuses on telemetry features, including metrics, logging, and tracing.
- **Setup Requirements**: Requires the Istio control plane to be initialized with telemetry configurations.

### Helm Tests

- **Component Focus**: Primarily focuses on deploying Istio using Helm charts.
- **Setup Requirements**: Requires the Istio control plane to be initialized using Helm charts.

### Security Tests

- **Component Focus**: Primarily focuses on security features and components, such as authentication and authorization.
- **Setup Requirements**: Requires the Istio control plane to be initialized with security configurations.

### Implications of Test Setup

- **Resource Allocation**: Ensure that the necessary resources (e.g., pods, namespaces) are allocated for the components being tested.
- **Isolation**: Tests should be isolated to prevent interference between different components and test cases.
- **Scalability**: The test setup should be scalable to accommodate additional tests and components in the future.

## Conclusion

By following these guidelines and understanding the architectural considerations, you can effectively add integration tests to the Istio project. Ensure that you choose the appropriate folder based on the components you are testing and understand the implications of the main test setup in each folder.
