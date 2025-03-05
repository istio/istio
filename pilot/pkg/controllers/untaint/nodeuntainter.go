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

package untaint

import (
	"encoding/json"
	"fmt"

	"gomodules.xyz/jsonpatch/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/labels"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
)

var log = istiolog.RegisterScope("untaint", "CNI node-untaint controller")

const (
	TaintName = "cni.istio.io/not-ready"
)

var istioCniLabels = map[string]string{
	"k8s-app": "istio-cni-node",
}

type NodeUntainter struct {
	podsClient  kclient.Client[*v1.Pod]
	nodesClient kclient.Client[*v1.Node]
	cnilabels   labels.Instance
	ourNs       string
	queue       controllers.Queue
}

func filterNamespace(ns string) func(any) bool {
	return func(obj any) bool {
		object := controllers.ExtractObject(obj)
		if object == nil {
			return false
		}
		return ns == object.GetNamespace()
	}
}

func NewNodeUntainter(stop <-chan struct{}, kubeClient kubelib.Client, cniNs, sysNs string, debugger *krt.DebugHandler) *NodeUntainter {
	log.Debugf("starting node untainter with labels %v", istioCniLabels)
	ns := cniNs
	if ns == "" {
		ns = sysNs
	}
	podsClient := kclient.NewFiltered[*v1.Pod](kubeClient, kclient.Filter{
		ObjectFilter:    kubetypes.NewStaticObjectFilter(filterNamespace(ns)),
		ObjectTransform: kubelib.StripPodUnusedFields,
	})
	nodes := kclient.NewFiltered[*v1.Node](kubeClient, kclient.Filter{ObjectTransform: kubelib.StripNodeUnusedFields})
	nt := &NodeUntainter{
		podsClient:  podsClient,
		nodesClient: nodes,
		cnilabels:   labels.Instance(istioCniLabels),
		ourNs:       ns,
	}
	nt.setup(stop, debugger)
	return nt
}

func (n *NodeUntainter) setup(stop <-chan struct{}, debugger *krt.DebugHandler) {
	opts := krt.NewOptionsBuilder(stop, "node-untaint", debugger)
	nodes := krt.WrapClient[*v1.Node](n.nodesClient, opts.WithName("nodes")...)
	pods := krt.WrapClient[*v1.Pod](n.podsClient, opts.WithName("pods")...)

	readyCniPods := krt.NewCollection(pods, func(ctx krt.HandlerContext, p *v1.Pod) **v1.Pod {
		log.Debugf("cniPods event: %s", p.Name)
		if p.Namespace != n.ourNs {
			return nil
		}
		if !n.cnilabels.SubsetOf(p.ObjectMeta.Labels) {
			return nil
		}
		if !IsPodReadyConditionTrue(p.Status) {
			return nil
		}
		log.Debugf("pod %s on node %s ready!", p.Name, p.Spec.NodeName)
		return &p
	}, opts.WithName("cni-pods")...)

	// these are all the nodes that have a ready cni pod. if the cni pod is ready,
	// it means we are ok scheduling pods to it.
	readyCniNodes := krt.NewCollection(readyCniPods, func(ctx krt.HandlerContext, p *v1.Pod) **v1.Node {
		pnode := krt.FetchOne(ctx, nodes, krt.FilterKey(p.Spec.NodeName))
		if pnode == nil {
			return nil
		}
		node := *pnode
		if !hasTaint(node) {
			return nil
		}
		return &node
	}, opts.WithName("ready-cni-nodes")...)

	n.queue = controllers.NewQueue("untaint nodes",
		controllers.WithReconciler(n.reconcileNode),
		controllers.WithMaxAttempts(5))

	// remove the taints from readyCniNodes
	readyCniNodes.Register(func(o krt.Event[*v1.Node]) {
		if o.Event == controllers.EventDelete {
			return
		}
		if o.New != nil {
			log.Debugf("adding node to queue event: %s", (*o.New).Name)
			n.queue.AddObject(*o.New)
		}
	})
}

func (n *NodeUntainter) HasSynced() bool {
	return n.queue.HasSynced()
}

func (n *NodeUntainter) Run(stop <-chan struct{}) {
	kubelib.WaitForCacheSync("node untainer", stop, n.nodesClient.HasSynced, n.podsClient.HasSynced)
	n.queue.Run(stop)
	n.podsClient.ShutdownHandlers()
	n.nodesClient.ShutdownHandlers()
}

func (n *NodeUntainter) reconcileNode(key types.NamespacedName) error {
	log.Debugf("reconciling node %s", key.Name)
	node := n.nodesClient.Get(key.Name, key.Namespace)
	if node == nil {
		return nil
	}

	err := removeReadinessTaint(n.nodesClient, node)
	if err != nil {
		log.Errorf("failed to remove readiness taint from node %v: %v", node.Name, err)
	}
	return err
}

func removeReadinessTaint(nodesClient kclient.Client[*v1.Node], node *v1.Node) error {
	updatedTaint := deleteTaint(node.Spec.Taints)
	if len(updatedTaint) == len(node.Spec.Taints) {
		// nothing to remove..
		return nil
	}

	log.Debugf("removing readiness taint from node %v", node.Name)
	atomicTaintRemove := []jsonpatch.Operation{
		// make sure taints hadn't chained since we last read the node
		{
			Operation: "test",
			Path:      "/spec/taints",
			Value:     node.Spec.Taints,
		},
		// remove the taint
		{
			Operation: "replace",
			Path:      "/spec/taints",
			Value:     updatedTaint,
		},
	}
	patch, err := json.Marshal(atomicTaintRemove)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %v", err)
	}

	_, err = nodesClient.Patch(node.Name, "", types.JSONPatchType, patch)
	if err != nil {
		return fmt.Errorf("failed to update node %v after adding taint: %v", node.Name, err)
	}
	log.Debugf("removed readiness taint from node %v", node.Name)
	return nil
}

// deleteTaint removes all the taints that have the same key and effect to given taintToDelete.
func deleteTaint(taints []v1.Taint) []v1.Taint {
	newTaints := []v1.Taint{}
	for i := range taints {
		if taints[i].Key == TaintName {
			continue
		}
		newTaints = append(newTaints, taints[i])
	}
	return newTaints
}

func hasTaint(n *v1.Node) bool {
	for _, taint := range n.Spec.Taints {
		if taint.Key == TaintName {
			return true
		}
	}
	return false
}

// IsPodReady is copied from kubernetes/pkg/api/v1/pod/utils.go
func IsPodReady(pod *v1.Pod) bool {
	return IsPodReadyConditionTrue(pod.Status)
}

// IsPodReadyConditionTrue returns true if a pod is ready; false otherwise.
func IsPodReadyConditionTrue(status v1.PodStatus) bool {
	condition := GetPodReadyCondition(status)
	return condition != nil && condition.Status == v1.ConditionTrue
}

func GetPodReadyCondition(status v1.PodStatus) *v1.PodCondition {
	_, condition := GetPodCondition(&status, v1.PodReady)
	return condition
}

func GetPodCondition(status *v1.PodStatus, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if status == nil {
		return -1, nil
	}
	return GetPodConditionFromList(status.Conditions, conditionType)
}

// GetPodConditionFromList extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodConditionFromList(conditions []v1.PodCondition, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}
