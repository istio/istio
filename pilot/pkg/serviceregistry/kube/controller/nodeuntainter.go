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

package controller

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
)

const (
	TaintName = "cni.istio.io/not-ready"
)

type nodeUntainter struct {
	podsClient  kclient.Client[*v1.Pod]
	nodesClient kclient.Client[*v1.Node]
	cnilabels   map[string]string
	ourNs       string
	queue       controllers.Queue
}

func newNodeUntainter(c *Controller) *nodeUntainter {
	labels := map[string]string{
		"k8s-app": "istio-cni-node",
	}
	log.Debugf("starting node untainter with labels %v", labels)
	ns := c.opts.CniNamespace
	if ns == "" {
		ns = c.opts.SystemNamespace
	}
	nt := &nodeUntainter{
		podsClient:  c.podsClient,
		nodesClient: c.nodes,
		cnilabels:   labels,
		ourNs:       ns,
	}
	nt.setup()
	return nt
}

func (n *nodeUntainter) setup() {
	nodes := krt.WrapClient[*v1.Node](n.nodesClient)
	// as we fetch all pods anyway, no need to create new informer..
	pods := krt.WrapClient[*v1.Pod](n.podsClient)

	labelInst := labels.Instance(n.cnilabels)
	readyCniPods := krt.NewCollection(pods, func(ctx krt.HandlerContext, p *v1.Pod) *v1.Pod {
		log.Debugf("cniPods event: %s", p.Name)
		if p.Namespace != n.ourNs {
			return nil
		}
		if !labelInst.SubsetOf(p.ObjectMeta.Labels) {
			return nil
		}
		if !IsPodReadyConditionTrue(p.Status) {
			return nil
		}
		log.Debugf("pod %s on node %s ready!", p.Name, p.Spec.NodeName)
		return p
	})

	// these are all the nodes that have a ready cni pod. if the cni pod is ready,
	// it means we are ok scheduling pods to it.
	readyCniNodes := krt.NewCollection(readyCniPods, func(ctx krt.HandlerContext, p v1.Pod) *v1.Node {
		pnode := krt.FetchOne(ctx, nodes, krt.FilterKey(p.Spec.NodeName))
		if pnode == nil {
			return nil
		}
		node := *pnode
		if !hasTaint(node) {
			return nil
		}
		return node
	})

	n.queue = controllers.NewQueue("untaint nodes",
		controllers.WithReconciler(n.reconcileNode),
		controllers.WithMaxAttempts(5))

	// remove the taints from readyCniNodes
	readyCniNodes.Register(func(o krt.Event[v1.Node]) {
		if o.Event == controllers.EventDelete {
			return
		}
		if o.New != nil {
			log.Debugf("adding node to queue event: %s", o.New.Name)
			n.queue.AddObject(o.New)
		}
	})
}

func (n *nodeUntainter) Run(stop <-chan struct{}) {
	n.queue.Run(stop)
}

func (n *nodeUntainter) reconcileNode(key types.NamespacedName) error {
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
	updatedTaint := deleteTaint(node.Spec.Taints, &v1.Taint{Key: TaintName, Effect: v1.TaintEffectNoSchedule})
	if len(updatedTaint) == len(node.Spec.Taints) {
		return nil
	}

	log.Debugf("removing readiness taint from node %v", node.Name)
	node.Spec.Taints = updatedTaint
	_, err := nodesClient.Update(node)
	if err != nil {
		return fmt.Errorf("failed to update node %v after adding taint: %v", node.Name, err)
	}
	log.Debugf("removed readiness taint from node %v", node.Name)
	return nil
}

// deleteTaint removes all the taints that have the same key and effect to given taintToDelete.
func deleteTaint(taints []v1.Taint, taintToDelete *v1.Taint) []v1.Taint {
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
