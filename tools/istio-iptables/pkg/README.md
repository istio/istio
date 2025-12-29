# istio-iptables

This is an `iptables` wrapper library for Istio. It is similar in basic concept to [](https://github.com/kubernetes-sigs/iptables-wrappers) but Istio needs to invoke `iptables` from a more varied set of contexts than K8s, and so cannot simply rely on "default" binary aliases to `iptables`/`iptables-save`/`iptables-restore`

This wrapper solves for that by fixing the wrapper lib to have binary detection logic that will work in *all* contexts where we use it, rely fully on binary autodetection in all spots, properly handling `iptables/ip6tables` variants..

The end result is that if this `istio-iptables` wrapper is used, the correct `iptables` binary should be selected dynamically, regardless of whether the context is a pod, the host, a container, or the host filesystem, or what "default" binary is configured in either the container or the host.

For context, there are 3 basic iptables binaries we use.

- iptables
- iptables-save
- iptables-restore

But with the kernel legacy/nft split, there are parallel binaries for each of those, and again for ipv6 tables, so the actual potential binary set on any given linux machine can be some combination of:

- iptables-legacy
- iptables-nft
- ip6tables
- iptables-legacy-save
- iptables-nft-save
- iptables-legacy-restore
- iptables-nft-restore
- ip6tables-legacy

...and on and on - the matrix is like 12 different binaries, potentially. Which is ridiculous but common.

We use our `iptables` wrapper lib 4 different ways across sidecar and ambient, and in each case "which iptables binary should we use" is a question with a different answer.

| Usage | Using iptables Binaries From | Networking context to select correct binary against |
| ------------- | ------------- | ------------- |
| from CNI plugin (sidecar) | host $PATH  | pod netns context |
| from init container  (sidecar) | init container $PATH  | pod netns context |
| from CNI agent (ambient) | CNI container $PATH  | pod netns context |
| from CNI agent (ambient) | CNI container $PATH  | host netns context |

If, for instance, the host has `iptables-legacy` and `iptables-nft` in $PATH, which should we use? We should see if rules are defined in `nft` at all and prefer that, but if no rules are in `nft` tables, and the `legacy` binary is available and rules are defined in `legacy`, we should use the `legacy` binary. If no rules are defined in either, we should use the system-default.

_If_ we are running directly on the host (not in a container) we can just assume whatever binary (legacy or nft) is aliased to `iptables` is correct and use that, and we're done. This is what k8s does for the kubelet.

However we do something a little more weird than K8S - we also have CNI, and that is running iptables from inside a container against the host netns. The CNI container ships its _own_ iptables binaries that may differ from the host. So we have to match the rules present in the host netns (which we can see) with the correct binary we have access to (only our own baked into the container) - we cannot rely purely on the K8S logic here because we are not only concerned with a kubelet-style context.

So basically selecting the right binary for one of the above cases is a heuristic with 2 inputs:

1. Does the particular binary variant (legacy, nft, ip6tables) even exist in $PATH?
1. In the current netns context, are there already rules defined in one of (legacy, nft) tables? If so, prefer that one.

Fundamentally, calling code should _never_ make assumptions about which specific `iptables` binary is selected or used or what "the system default" is or should be, and should simply declare the type of operation (`insert/save/restore`) and let the wrapper detection logic in this package pick the correct binary given the environmental context.

## Reconciliation and Idempotency handling

This wrapper implements reconciliation logic to ensure that `iptables` rules are applied correctly and idempotently across two primary scenarios:

- When Istio `iptables` rules are applied to a preexisting, running pod that already has (potentially outdated or incomplete) in-pod rules from a previous version or instance of Istio
- When Istio `iptables` rules are applied to a pod that has preexisting in-pod `iptables` rules from a component other than Istio

The reconciliation logic _primarily_ modifies rules in chains prefixed with `ISTIO_` and manages jumps from primary tables to those chains. In some rare cases, it may also modify some non-jump rules from non-ISTIO chains that were deployed by certain istio-iptables configurations (e.g., tproxy). However, this is the exception rather than the norm, as we are actively working to update Istio to eliminate the use of non-jump rules in non-ISTIO chains moving forward.

The logic of istio-iptables is primarily controlled by the `Reconcile` flag, which determines whether preexisting, incompatible iptables rules need to be reconciled when a drift from the desired state is detected.
Depending on the value of the flag, different behaviors can be observed:
- **`Reconcile=true`**: If reconciliation is enabled, then the `istio-iptables` may attempt to reconcile existing iptables. In particular, when the flag is enabled, the following scenario will occur:
  - If no existing rules are found, the wrapper will apply the new rules and chains. This is a typical first-time installation.
  - If existing rules are found and are equivalent to the desired outcome, no new rules will be applied, and the process will successfully terminate.
  - If existing rules are found but they are not equivalent to the desired outcome (could be partial or simply different), the wrapper will attempt to reconcile them by perform the following operations:
    - Sets up guardrails (iptables rules that drop all inbound and outbound traffic to prevent traffic escape during the update)
    - Performs cleanup of existing rules
    - Applies new rules
    - Removes guardrails
- **`Reconcile=false`**: If reconciliation is disabled, the `istio-iptables` wrapper will not perform existing changes to preexisting rules if found. The following will occur:
  - If no existing rules are found, the wrapper will apply the new ones.
  - If existing rules are found and are equivalent to the desired outcome, no new rules will be applied, and the process will successfully terminate.
  - If existing rules are found but they are not equivalent to the desired outcome (could be partial or simply different), the wrapper will attempt to apply the new rules but the outcome is not guaranteed.

`istio-iptables` also offers a cleanup-only mode, controlled by the `CleanupOnly` flag. When set to true, only cleanup operations are performed, without applying new rules or setting up guardrails.

### Iptables Cleanup Differences across CNI node agent, Privileged Init Container, and VM

Istio supports three different mechanisms for applying `iptables` rules inside pods:
1. A privileged init container injected into all pods with an Istio sidecar, via mutating webhook (used only in sidecars, not recommended if privileged init containers are a security concern) which runs on pod startup and inserts the rules.
1. Istio VM mode, where the Istio agent runs on a virtual machine and configures `iptables` rules to intercept traffic for services on that VM.
1. A privileged node agent daemonset, that steps into pod network namespaces and inserts/removes the rules (if optionally used with sidecar dataplane mode, replaces the privileged init container approach. Required/non-optional for ambient dataplane mode)

When `istio-cni` Node Agent is in charge of applying the `iptables` rules, a two-pass cleanup logic is needed to ensure the correctness of the final outcome:
- **First Pass**: Reverses all rules in the expected/desired state. This will effectively remove all the ISTIO chains and rules from the expected state. It will also remove some non-jump rules in non-ISTIO chains if the expected state includes them.
- **Second Pass**: Only performed if `Reconcile=true`. The wrapper:
  1. Rechecks the current iptables state
  1. Attempts to delete any jump rule to an Istio chain
  1. Removes any remaining `ISTIO_*` chains

The second pass is crucial when the `istio-cni` Node Agent manages iptables because workloads may have been configured by different versions of the Node Agent, or with different `iptables` configurations.
For all other use cases, only the first pass is performed, as reruns in `istio-init`/VM can only occur and those always involve a current state that's a subset of the expected state.

### Limitations and guidelines

1. **Order Insensitivity**:  Two `iptables` rules snapshots are considered identical if they share exactly the same rules, even if these rules appear in different orders.
1. **Non-Istio Chain Rules**: Two `iptables` rules snapshots are considered identical if the only difference is that the current snapshot has rules in non-ISTIO chains that are absent in the expected/desired snapshot. This includes jump rules pointing to `ISTIO_*` chains in `OUTPUT`, `INPUT`, etc.
1. **Cleanup Scope**: Second-pass cleanup can only remove leftover Istio chains and jumps to those chains. Any other non-jump rule in non-Istio chains will remain, as there's no reliable way to determine if it was created by Istio or the user.

To avoid issues with the limitations above, the maintainers needs to keep the following guidelines when reviewing changes regarding `iptables` configurations:
1. Istio rules should ALWAYS be added to a chain prefixed with `ISTIO_` - even if it's just for one rule. Istio code should not insert `iptables` rules into main tables, or non-prefixed chains. The sole exception to this are the required JUMP rules from the main tables to the recommended `ISTIO_` prefixed custom chains.
1. Ensure that differences between configurations involve more than just rules in non-ISTIO chains (including jump rules to `ISTIO_*` chains).
1. Make sure that configuration differences are more than just the order of the rules.
