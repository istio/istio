// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ca

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/security/pkg/server/ca/authenticate/kubeauth"
)

// NodeAuthorizer is a component that implements a subset of Kubernetes Node Authorization
// (https://kubernetes.io/docs/reference/access-authn-authz/node/) for Istio CA. Specifically, it
// validates that a node proxy which requests certificates for workloads on its own node is requesting
// valid identities which run on that node (rather than arbitrary ones).
type NodeAuthorizer interface {
	authenticateImpersonation(ctx context.Context, caller security.KubernetesInfo, requestedIdentityString string) error
}

// MulticlusterNodeAuthorizor is an implementation of NodeAuthorizer for multi-cluster environmnets.
// please refer to https://docs.google.com/document/d/10uf4EvUVif4xGeCYQydaKh9Yaz9wpysao7gyLewJY2Q.
// It is responsible for maintaining an index of node authenticators, one per cluster.
// node authorizations from one cluster will be forwarded to the node authorizer for the same cluster.
type MulticlusterNodeAuthorizor struct {
	remoteNodeAuthenticators map[cluster.ID]*ClusterNodeAuthorizer
	m                        sync.Mutex
	filter                   namespace.DiscoveryFilter
	trustedNodeAccounts      sets.Set[types.NamespacedName]
}

func NewMulticlusterNodeAuthenticator(filter namespace.DiscoveryFilter, trustedNodeAccounts sets.Set[types.NamespacedName],
	addClusterHandler func(multicluster.ClusterHandler),
) *MulticlusterNodeAuthorizor {
	m := &MulticlusterNodeAuthorizor{
		remoteNodeAuthenticators: map[cluster.ID]*ClusterNodeAuthorizer{},
		filter:                   filter,
		trustedNodeAccounts:      trustedNodeAccounts,
	}
	if addClusterHandler != nil {
		addClusterHandler(m)
	}
	return m
}

func (mNa *MulticlusterNodeAuthorizor) ClusterAdded(cluster *multicluster.Cluster, _ <-chan struct{}) {
	na, err := NewClusterNodeAuthorizer(cluster.Client, mNa.filter, mNa.trustedNodeAccounts)
	if err != nil {
		serverCaLog.Errorf("failed to initialize node authorizer for cluster %v: %v", cluster.ID, err)
		return
	}
	mNa.m.Lock()
	defer mNa.m.Unlock()
	mNa.addCluster(cluster.ID, na)
}

func (mNa *MulticlusterNodeAuthorizor) ClusterUpdated(cluster *multicluster.Cluster, stop <-chan struct{}) {
	na, err := NewClusterNodeAuthorizer(cluster.Client, mNa.filter, mNa.trustedNodeAccounts)
	if err != nil {
		serverCaLog.Errorf("failed to initialize node authorizer for cluster %v: %v", cluster.ID, err)
	}
	mNa.m.Lock()
	defer mNa.m.Unlock()
	mNa.deleteCluster(cluster.ID)
	mNa.addCluster(cluster.ID, na)
}

func (mNa *MulticlusterNodeAuthorizor) ClusterDeleted(key cluster.ID) {
	mNa.m.Lock()
	defer mNa.m.Unlock()
	mNa.deleteCluster(key)
}

func (mNa *MulticlusterNodeAuthorizor) addCluster(clusterID cluster.ID, na *ClusterNodeAuthorizer) {
	mNa.remoteNodeAuthenticators[clusterID] = na
}

func (mNa *MulticlusterNodeAuthorizor) deleteCluster(clusterID cluster.ID) {
	delete(mNa.remoteNodeAuthenticators, clusterID)
}

func (mNa *MulticlusterNodeAuthorizor) authenticateImpersonation(ctx context.Context, caller security.KubernetesInfo, requestedIdentityString string) error {
	clusterID := kubeauth.ExtractClusterID(ctx)
	na, found := mNa.remoteNodeAuthenticators[clusterID]
	if !found {
		return fmt.Errorf("no node authorizer for cluster %v", clusterID)
	}
	return na.authenticateImpersonation(ctx, caller, requestedIdentityString)
}

// ClusterNodeAuthorizer is an implementation of NodeAuthorizer for single-cluster environment.
// Within a single cluster, it validates that a node proxy which requests certificates for
// workloads on its own node is requesting valid identities which run on that node.
type ClusterNodeAuthorizer struct {
	trustedNodeAccounts sets.Set[types.NamespacedName]
	pods                kclient.Client[*v1.Pod]
	nodeIndex           *kclient.Index[SaNode, *v1.Pod]
}

func NewClusterNodeAuthorizer(client kube.Client, filter namespace.DiscoveryFilter,
	trustedNodeAccounts sets.Set[types.NamespacedName],
) (*ClusterNodeAuthorizer, error) {
	pods := kclient.NewFiltered[*v1.Pod](client, kclient.Filter{
		ObjectFilter:    filter,
		ObjectTransform: kube.StripPodUnusedFields,
	})
	// Add an Index on the pods, storing the service account and node. This allows us to later efficiently query.
	index := kclient.CreateIndex[SaNode, *v1.Pod](pods, func(pod *v1.Pod) []SaNode {
		if len(pod.Spec.NodeName) == 0 {
			return nil
		}
		if len(pod.Spec.ServiceAccountName) == 0 {
			return nil
		}
		return []SaNode{{
			ServiceAccount: types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Spec.ServiceAccountName,
			},
			Node: pod.Spec.NodeName,
		}}
	})
	return &ClusterNodeAuthorizer{
		pods:                pods,
		nodeIndex:           index,
		trustedNodeAccounts: trustedNodeAccounts,
	}, nil
}

func (na *ClusterNodeAuthorizer) authenticateImpersonation(ctx context.Context, caller security.KubernetesInfo, requestedIdentityString string) error {
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
	callerPod := na.pods.Get(caller.PodName, caller.PodNamespace)
	if callerPod == nil {
		return fmt.Errorf("pod %v/%v not found", caller.PodNamespace, caller.PodName)
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
	res := na.nodeIndex.Lookup(k)
	// We don't care what pods are part of the index, only that there is at least one. If there is one,
	// it is appropriate for the caller to request this identity.
	if len(res) == 0 {
		return fmt.Errorf("no instances of %q found on node %q", k.ServiceAccount, k.Node)
	}
	serverCaLog.Debugf("Node caller %v impersonated %v", caller, requestedIdentityString)
	return nil
}
