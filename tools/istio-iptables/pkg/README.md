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

This wrapper implements reconciliation logic to ensure that iptables rules are applied correctly and idempotently across different scenarios (e.g., upgrade and istio-init reruns).
Due to this logic the wrapper will perform the apply and/or a cleanup of iptables depending on the current scenario that has been detected.

The following behaviors can normally occurr:
1. **No Delta Detection**: If iptables execution detects no difference between the current and expected/desired state, it skips the apply step unless the `ForceApply` flag is set to true.
2. **First-Time Installation**: If a delta is detected and no "residue" of previous Istio-related rules exists, it initiates a clean installation of the rules which involves only the apply step.
3. **Update Existing Rules**: If the current state differs from the desired state and previous Istio executions are detected, the wrapper:
   - Sets up guardrails (iptables rules that drop all inbound and outbound traffic to prevent traffic escape during the update)
   - Performs cleanup of existing rules
   - Applies new rules
   - Removes guardrails
4. **Cleanup-Only Mode**: If the `CleanupOnly` flag is set to true, only cleanup operations are performed, without applying new rules or setting up guardrails.

### CNI vs. non-CNI Cleanup Differences

The cleanup logic differs between the CNI and non-CNI use cases.
For the CNI, a two-pass cleanup logic is needed to ensure the correctness of the final outcome:
- **First Pass**: Reverses all rules in the expected/desired state. This may delete non-jump rules in non-Istio chains.
- **Second Pass**: Only performed if `Reconcile=true`. The wrapper:
  1. Rechecks the current iptables state
  2. Attempts to delete any jump rule to an Istio chain
  3. Removes any remaining `ISTIO_*` chains
  4. Uses parsing of the `--jump` field in iptables-save output
 
The second pass is essential for CNI because workloads might have been enrolled by different Istio versions or instances with different iptables configurations.
For non-CNI use cases, only the first pass is performed as in `istio-init` only reruns can occur and those always involve a current state that's a subset of the expected state.

### Limitations and guidelines

1. **Order Insensitivity**: States are considered identical if they share the same rules, even if these rules appear in different orders.
2. **Non-Istio Chain Rules**: Two states are considered identical if the only difference is that the current state has rules in non-ISTIO chains that are absent in the expected/desired state. This includes jump rules pointing to `ISTIO_*` chains in `OUTPUT`, `INPUT`, etc.
3. **Cleanup Scope**: Second-pass cleanup can only remove leftover Istio chains and jumps to those chains. Any other non-jump rule in non-Istio chains will remain, as there's no reliable way to determine if it was created by Istio or the user.

To avoid issues with the limitations above, the following guidelines needs to be kept in mind:
1. Avoid using non-jump rules in non-ISTIO chains, as these can be difficult ```
(or rather impossible...) to clean if they exist in some configurations but not others.
2. Ensure that differences between configurations involve more than just rules in non-ISTIO chains (including jump rules to `ISTIO_*` chains).
3. Make sure that configuration differences are more than just the order of the rules.
