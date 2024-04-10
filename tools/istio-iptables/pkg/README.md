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
