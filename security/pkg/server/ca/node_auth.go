package ca

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
)

// NodeAuthorizer is a component that implements a subset of Kubernetes Node Authorization
// (https://kubernetes.io/docs/reference/access-authn-authz/node/) for Istio CA. Specifically, it
// validates that a node proxy which requests certificates for workloads on its own node is request
// valid identities which run on that node (rather than arbitrary ones).
type NodeAuthorizer struct {
	podLister           listerv1.PodLister
	podIndexer          cache.Indexer
	trustedNodeAccounts map[types.NamespacedName]struct{}
}

const NodeSaIndex = "node+sa"

func NewNodeAuthorizer(client kube.Client, trustedNodeAccounts map[types.NamespacedName]struct{}) (*NodeAuthorizer, error) {
	pods := client.KubeInformer().Core().V1().Pods()

	// Add an Index on the pods, storing the service account and node. This allows us to later efficiently query.
	if err := pods.Informer().AddIndexers(map[string]cache.IndexFunc{
		NodeSaIndex: func(obj interface{}) ([]string, error) {
			pod, ok := obj.(*v1.Pod)
			if !ok {
				return nil, nil
			}
			if len(pod.Spec.NodeName) == 0 {
				return nil, nil
			}
			if len(pod.Spec.ServiceAccountName) == 0 {
				return nil, nil
			}
			return []string{SaNode{
				ServiceAccount: types.NamespacedName{
					Namespace: pod.Namespace,
					Name:      pod.Spec.ServiceAccountName,
				},
				Node: pod.Spec.NodeName,
			}.String()}, nil
		},
	}); err != nil {
		// This should only happen if the informer has already started.
		// This can only happen if a component started before the CA already registers the informer *and* starts it.
		// This should not happen; if it does, the CA will not function properly, and its likely a programming error, so returning
		// an error here (which will ultimately exit the process) is appropriate.
		return nil, fmt.Errorf("failed to add indexer: %v", err)
	}
	return &NodeAuthorizer{
		podLister:           pods.Lister(),
		podIndexer:          pods.Informer().GetIndexer(),
		trustedNodeAccounts: trustedNodeAccounts,
	}, nil
}

func (na *NodeAuthorizer) authenticateImpersonation(caller security.KubernetesInfo, requestedIdentityString string) error {
	callerSa := types.NamespacedName{
		Namespace: caller.PodNamespace,
		Name:      caller.PodServiceAccount,
	}
	// First, make sure the caller is allowed to impersonate, in general
	if _, f := na.trustedNodeAccounts[callerSa]; !f {
		return fmt.Errorf("caller (%v) is not allowed to impersonate", caller)
	}
	// Next, make sure the identity they want to impersonate is valid, in general
	requestedIdentity, err := spiffe.ParseIdentity(requestedIdentityString)
	if err != nil {
		return fmt.Errorf("failed to validate impersonated identity %v", requestedIdentityString)
	}

	// Finally, we validate the requested identity is running on the same node the caller is on
	callerPod, err := na.podLister.Pods(caller.PodNamespace).Get(caller.PodName)
	if err != nil {
		return fmt.Errorf("failed to lookup pod: %v", err)
	}
	// Make sure UID is still valid for our current state
	if callerPod.UID != types.UID(caller.PodUID) {
		// This would only happen if a pod is re-created with the same name, and the CSR client is not in sync on which is current;
		// this is fine and should be eventually consistent. Client is expected to retry in this case.
		return fmt.Errorf("pod found, but UID does not match: %v vs %v", callerPod.UID, caller.PodUID)
	}
	if callerPod.Spec.ServiceAccountName != caller.PodServiceAccount {
		// This should never happen, but just in case add an additional check
		return fmt.Errorf("pod found, but ServiceAccount does not match: %v vs %v", callerPod.Spec.ServiceAccountName, caller.PodServiceAccount)
	}
	// We want to find out if there is any pod running with the requested identity on the callers node.
	// The indexer (previously setup) creates a lookup table for a {Node, SA} pair, which we can lookup
	k := SaNode{
		ServiceAccount: types.NamespacedName{Name: requestedIdentity.ServiceAccount, Namespace: requestedIdentity.Namespace},
		Node:           callerPod.Spec.NodeName,
	}
	// TODO: this is currently single cluster; we will need to take the cluster of the proxy into account
	// to support multi-cluster properly.
	res, err := na.podIndexer.ByIndex(NodeSaIndex, k.String())
	if err != nil {
		// This should never happen (only possible if the index doesn't exist)
		return fmt.Errorf("failed to find node local service accounts: %v", err)
	}
	// We don't care what pods are part of the index, only that there is at least one. If there is one,
	// it is appropriate for the caller to request this identity.
	if len(res) == 0 {
		return fmt.Errorf("no instances of %q found on node %q", k.ServiceAccount, k.Node)
	}
	serverCaLog.Debugf("Node caller %v impersonated %v", caller, requestedIdentityString)
	return nil
}
