# Istio `nftables` backend

This document outlines the design, configuration, and basic troubleshooting steps for the `nftables` backend in Istio.
As the official successor to `iptables`, `nftables` offers a modern, high-performance alternative for transparently redirecting
traffic to and from the `Envoy` sidecar proxy. Many major Linux distributions are actively moving towards adopting native
`nftables` support. At present, this backend supports Istio sidecar mode only, with ambient mode support currently under development.

## Key Benefits

- **Unified IPv4/IPv6 Ruleset**: Uses the `inet` family to handle both IPv4 and IPv6 traffic through a single ruleset, simplifying
    configuration and avoiding the need to manage `iptables` and `ip6tables` separately.
- **Atomic Rule Updates**: Takes advantage of `nftables` built-in support for atomic transactions, allowing rules to be updated safely
    without risking partial or inconsistent network states during changes.
- **Performance**: Overcomes the scalability issues seen with iptables, where performance tends to drop as the number of rules grows.

## Architecture and Design

The code builds the `nftables` ruleset in memory and applies it atomically in a single transaction. This ensures the entire configuration
is applied as one unit, avoiding any partially updated or broken states.

In the `nftables` implementation, custom tables and chains are created, but they follow a naming convention similar to the one used
with iptables. This makes the setup easier to troubleshoot and maintain, especially for those already familiar with the iptables approach.

This implementation uses the [knftables](https://github.com/kubernetes-sigs/knftables) library, which is commonly used in the K8s ecosystem
for working with nftables.

```sh
table inet istio-proxy-nat {
    chain prerouting {
        type nat hook prerouting priority dstnat; policy accept;
        # Istio redirection rules...
    }

    chain output {
        type nat hook output priority dstnat; policy accept;
        # Istio output rules...
    }

    # Custom Istio chains for traffic processing
    chain istio-inbound { }
    chain istio-output { }
    chain istio-redirect { }
    # ...additional chains
}
```

## Configuration

The `nftables` backend is **disabled by default**. To enable it, you must set the following value during installation process:

```sh
--set values.global.nativeNftables=true
```

This flag configures Istio to use the `nftables` backend instead of `iptables` for traffic redirection.

## Implementation Details

### Table Structure

The `nftables` backend creates three main tables in the `inet` family:

- **`istio-proxy-nat`**: Handles traffic redirection using DNAT/REDIRECT targets
- **`istio-proxy-mangle`**: Manages packet marking and TPROXY operations
- **`istio-proxy-raw`**: Handles connection tracking zones for DNS traffic

### Chain Organization

Each table contains both netfilter hook chains and custom chains:

**Hook/Base Chains** (attach to kernel netfilter hooks):
- `prerouting`: Processes incoming packets before routing decisions
- `output`: Processes locally-generated packets

**Custom Chains** (called via jump rules):
- `istio-inbound`: Inbound traffic processing logic
- `istio-output`: Outbound traffic processing logic
- `istio-redirect`: Common redirection target for Envoy
- `istio-tproxy`: TPROXY-specific handling
- `istio-divert`: Traffic diversion based on packet marking

### Rule Application Process

1. **Rule Generation**: The `NftablesRuleBuilder` constructs rules based on Istio configuration
1. **Transaction Creation**: All rules are assembled into a single `knftables.Transaction`
1. **Atomic Application**: The entire transaction is applied atomically via the `knftables` library, which internally validates rule structure and uses `nft -f -` for execution
1. **Error Handling**: On failure, no partial state is left behind

This ensures that the traffic redirection is never in an inconsistent state during updates.

## Reconciliation Logic

The implementation generates a **complete `nftables` ruleset** in memory and then applies it as a single, atomic transaction using knftables APIs.
It **does not** patch or modify individual rules on the fly. This ensures the ruleset is always consistent and valid.
The implementation also ensures that old rules are cleaned up and new rules are created at the same time, avoiding any partial updates.

## Debugging Guidelines

The following `nft` commands are useful for troubleshooting. The implementation includes `counters` on all rules to provide useful debugging information.

### View the entire ruleset with counters

```bash
nft list ruleset
```

**What to check**: Look at the `counter` values on rules in the `istio-inbound`, `istio-output` and related chains.
If the packet counts are non-zero, it means traffic is processed by that rule.

### View specific table

```bash
nft list table inet istio-proxy-nat
```

### View specific chain

```bash
nft list chain inet istio-proxy-nat istio-output
```

### Reset counters for fresh debugging

```bash
nft reset counters table inet istio-proxy-nat
```

## Limitations and Known Issues

- **Supports only sidecar mode with debug images:**. The `nft` binary is currently available only in the debug version of the Istio proxy
    and is not included in the [distroless image](https://github.com/istio/istio/issues/57237).
- **nftables version**: Requires `nft` binary version 1.0.1 or later.
