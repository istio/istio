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
	"sync"
	"time"

	"gomodules.xyz/jsonpatch/v2"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/klog/v2"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/labels"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

var log = istiolog.RegisterScope("untaint", "CNI node-untaint controller")

var istioCniLabels = map[string]string{
	"k8s-app": "istio-cni-node",
}

var ztunnelLabels = map[string]string{
	"app": "ztunnel",
}

// The DaemonSet controller adds tolerations for these to every DaemonSet pod, so they never keep one off a node.
var daemonSetDefaultTolerations = []v1.Toleration{
	{Key: v1.TaintNodeNotReady, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoExecute},
	{Key: v1.TaintNodeUnreachable, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoExecute},
	{Key: v1.TaintNodeDiskPressure, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
	{Key: v1.TaintNodeMemoryPressure, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
	{Key: v1.TaintNodePIDPressure, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
	{Key: v1.TaintNodeUnschedulable, Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
}

// How often nodes held back by the taint limit are retried.
var heldRetryInterval = 10 * time.Second

// How long a taint we added counts towards the limit before the node cache shows it.
const recentAddTTL = 30 * time.Second

type NodeUntainter struct {
	podsClient        kclient.Client[*v1.Pod]
	nodesClient       kclient.Client[*v1.Node]
	dsClient          kclient.Client[*appsv1.DaemonSet]
	cnilabels         labels.Instance
	ztunnellabels     labels.Instance
	cniNs             string
	ztunnelNs         sets.String
	checkZtunnel      bool
	taintWhenNotReady bool
	maxTaintedPercent int
	heldRetry         time.Duration
	queue             controllers.Queue
	taintName         string
	desired           krt.Collection[nodeTaint]
	cniDaemonSets     krt.Collection[*appsv1.DaemonSet]
	ztunnelDaemonSets krt.Collection[*appsv1.DaemonSet]

	mu sync.Mutex
	// nodes that need the taint but were held back by the limit
	held sets.String
	// nodes we tainted recently, which the node cache may not show yet
	recentAdds map[string]time.Time
}

// nodeTaint is the change a node needs: add the readiness taint, or remove it.
type nodeTaint struct {
	Node  string
	Taint bool
}

func (t nodeTaint) ResourceName() string {
	return t.Node
}

func filterNamespaces(nses sets.String) func(any) bool {
	return func(obj any) bool {
		object := controllers.ExtractObject(obj)
		if object == nil {
			return false
		}
		return nses.Contains(object.GetNamespace())
	}
}

func NewNodeUntainter(stop <-chan struct{}, kubeClient kubelib.Client, cniNs, sysNs string, debugger *krt.DebugHandler) *NodeUntainter {
	log.Debugf("starting node untainter with labels %v", istioCniLabels)
	ns := cniNs
	if ns == "" {
		ns = sysNs
	}
	// istio-cni, ztunnel and istiod can all live in different namespaces, so watch the
	// cni namespace plus wherever ztunnel runs (only needed when we gate on ztunnel).
	checkZtunnel := features.NodeUntaintCheckZtunnel
	ztunnelNs := sets.New[string]()
	if checkZtunnel {
		ztunnelNs = ztunnelNamespaces(sysNs)
	}
	watchedNs := sets.New(ns).Merge(ztunnelNs)
	podsClient := kclient.NewFiltered[*v1.Pod](kubeClient, kclient.Filter{
		ObjectFilter:    kubetypes.NewStaticObjectFilter(filterNamespaces(watchedNs)),
		ObjectTransform: kubelib.StripPodUnusedFields,
		FieldSelector:   "status.phase!=Failed",
	})
	nodes := kclient.NewFiltered[*v1.Node](kubeClient, kclient.Filter{ObjectTransform: kubelib.StripNodeUnusedFields})
	nt := &NodeUntainter{
		podsClient:        podsClient,
		nodesClient:       nodes,
		cnilabels:         labels.Instance(istioCniLabels),
		ztunnellabels:     labels.Instance(ztunnelLabels),
		cniNs:             ns,
		ztunnelNs:         ztunnelNs,
		checkZtunnel:      checkZtunnel,
		taintWhenNotReady: features.NodeTaintWhenNotReady,
		maxTaintedPercent: min(max(features.NodeTaintMaxPercent, 0), 100),
		heldRetry:         heldRetryInterval,
		taintName:         features.NodeUntaintTaintName,
		held:              sets.New[string](),
		recentAdds:        map[string]time.Time{},
	}
	if nt.taintWhenNotReady {
		nt.dsClient = kclient.NewFiltered[*appsv1.DaemonSet](kubeClient, kclient.Filter{
			ObjectFilter: kubetypes.NewStaticObjectFilter(filterNamespaces(watchedNs)),
		})
	}
	nt.setup(stop, debugger)
	return nt
}

// ztunnelNamespaces returns where ztunnel may run. CA_TRUSTED_NODE_ACCOUNTS is rendered from
// `trustedZtunnelNamespace`; istiod's own ns stays in so a bad guess can't wedge scheduling.
func ztunnelNamespaces(sysNs string) sets.String {
	res := sets.New(sysNs)
	for sa := range features.CATrustedNodeAccounts {
		if sa.Namespace != "" {
			res.Insert(sa.Namespace)
		}
	}
	log.Debugf("node untainter looking for ztunnel in namespaces %v", res)
	return res
}

func (n *NodeUntainter) setup(stop <-chan struct{}, debugger *krt.DebugHandler) {
	opts := krt.NewOptionsBuilder(stop, "node-untaint", debugger)
	nodes := krt.WrapClient[*v1.Node](n.nodesClient, opts.WithName("nodes")...)
	pods := krt.WrapClient[*v1.Pod](n.podsClient, opts.WithName("pods")...)

	cniPods := krt.NewCollection(pods, func(ctx krt.HandlerContext, p *v1.Pod) **v1.Pod {
		if p.Namespace != n.cniNs || !n.cnilabels.SubsetOf(p.ObjectMeta.Labels) {
			return nil
		}
		return &p
	}, opts.WithName("cni-pods")...)

	ztunnelPods := krt.NewCollection(pods, func(ctx krt.HandlerContext, p *v1.Pod) **v1.Pod {
		if !n.ztunnelNs.Contains(p.Namespace) || !n.ztunnellabels.SubsetOf(p.ObjectMeta.Labels) {
			return nil
		}
		return &p
	}, opts.WithName("ztunnel-pods")...)

	byNode := func(p *v1.Pod) []string {
		if p.Spec.NodeName == "" {
			return nil
		}
		return []string{p.Spec.NodeName}
	}
	cniByNode := krt.NewIndex(cniPods, "node", byNode)
	ztunnelByNode := krt.NewIndex(ztunnelPods, "node", byNode)

	if n.taintWhenNotReady {
		daemonSets := krt.WrapClient[*appsv1.DaemonSet](n.dsClient, opts.WithName("daemonsets")...)
		n.cniDaemonSets = krt.NewCollection(daemonSets, func(ctx krt.HandlerContext, ds *appsv1.DaemonSet) **appsv1.DaemonSet {
			if ds.Namespace != n.cniNs || !n.cnilabels.SubsetOf(ds.Spec.Template.Labels) {
				return nil
			}
			return &ds
		}, opts.WithName("cni-daemonsets")...)
		n.ztunnelDaemonSets = krt.NewCollection(daemonSets, func(ctx krt.HandlerContext, ds *appsv1.DaemonSet) **appsv1.DaemonSet {
			if !n.ztunnelNs.Contains(ds.Namespace) || !n.ztunnellabels.SubsetOf(ds.Spec.Template.Labels) {
				return nil
			}
			return &ds
		}, opts.WithName("ztunnel-daemonsets")...)
	}

	// The taint change each node needs, if any. Fetch registers dependencies, so this recomputes
	// whenever a node, its istio pods, or (when tainting) the DaemonSets change.
	n.desired = krt.NewCollection(nodes, func(ctx krt.HandlerContext, node *v1.Node) *nodeTaint {
		ready := anyPodReady(krt.Fetch(ctx, cniPods, krt.FilterIndex(cniByNode, node.Name)))
		if ready && n.checkZtunnel {
			ready = anyPodReady(krt.Fetch(ctx, ztunnelPods, krt.FilterIndex(ztunnelByNode, node.Name)))
		}
		tainted := n.hasTaint(node)
		if ready {
			if tainted {
				return &nodeTaint{Node: node.Name, Taint: false}
			}
			return nil
		}
		if tainted || !n.taintWhenNotReady {
			return nil
		}
		expected := n.anyRunsOnNode(krt.Fetch(ctx, n.cniDaemonSets), node)
		if !expected && n.checkZtunnel {
			expected = n.anyRunsOnNode(krt.Fetch(ctx, n.ztunnelDaemonSets), node)
		}
		if !expected {
			// the dataplane is not meant to run here, so it can never become ready
			return nil
		}
		log.Debugf("node %s should run the dataplane but it is not ready, tainting it", node.Name)
		return &nodeTaint{Node: node.Name, Taint: true}
	}, opts.WithName("node-taints")...)

	n.queue = controllers.NewQueue("untaint nodes",
		controllers.WithReconciler(n.reconcileNode),
		controllers.WithMaxAttempts(5))

	n.desired.Register(func(o krt.Event[nodeTaint]) {
		if o.Event == controllers.EventDelete {
			return
		}
		if o.New != nil {
			log.Debugf("adding node to queue event: %s", o.New.Node)
			n.queue.Add(types.NamespacedName{Name: o.New.Node})
		}
	})
}

func (n *NodeUntainter) HasSynced() bool {
	return n.queue.HasSynced()
}

func (n *NodeUntainter) Run(stop <-chan struct{}) {
	synced := []cache.InformerSynced{n.nodesClient.HasSynced, n.podsClient.HasSynced}
	if n.dsClient != nil {
		synced = append(synced, n.dsClient.HasSynced)
	}
	kubelib.WaitForCacheSync("node untainer", stop, synced...)
	if n.taintWhenNotReady {
		go n.retryHeld(stop)
	}
	n.queue.Run(stop)
	n.podsClient.ShutdownHandlers()
	n.nodesClient.ShutdownHandlers()
	if n.dsClient != nil {
		n.dsClient.ShutdownHandlers()
	}
}

func (n *NodeUntainter) reconcileNode(key types.NamespacedName) error {
	log.Debugf("reconciling node %s", key.Name)
	node := n.nodesClient.Get(key.Name, key.Namespace)
	if node == nil {
		return nil
	}
	want := n.desired.GetKey(key.Name)
	if want == nil {
		return nil
	}

	if want.Taint {
		if n.taintBudget() <= 0 {
			n.hold(node.Name)
			return nil
		}
		err := addReadinessTaint(n.nodesClient, node, n.taintName)
		if err != nil {
			log.Errorf("failed to add readiness taint '%v' to node %v: %v", n.taintName, node.Name, err)
			return err
		}
		n.mu.Lock()
		n.recentAdds[node.Name] = time.Now()
		n.mu.Unlock()
		return nil
	}
	err := removeReadinessTaint(n.nodesClient, node, n.taintName)
	if err != nil {
		log.Errorf("failed to remove readiness taint '%v' from node %v: %v", n.taintName, node.Name, err)
	}
	return err
}

// taintBudget returns how many more nodes may get the readiness taint. Many nodes going not ready
// at once (a dataplane rollout, a bad image, istiod down while ztunnel restarts) is a cluster-wide
// problem that tainting cannot fix, and tainting them all would stop scheduling everywhere.
func (n *NodeUntainter) taintBudget() int {
	tainted := sets.New[string]()
	for _, node := range n.nodesClient.List(metav1.NamespaceAll, klabels.Everything()) {
		if n.hasTaint(node) {
			tainted.Insert(node.Name)
		}
	}
	n.mu.Lock()
	for name, at := range n.recentAdds {
		if tainted.Contains(name) || time.Since(at) > recentAddTTL {
			delete(n.recentAdds, name)
			continue
		}
		tainted.Insert(name)
	}
	n.mu.Unlock()
	return n.maxTainted() - tainted.Len()
}

// maxTainted is the limit on tainted nodes: maxTaintedPercent of the nodes the dataplane should
// run on, and at least one.
func (n *NodeUntainter) maxTainted() int {
	var expected int32
	for _, ds := range n.cniDaemonSets.List() {
		expected = max(expected, ds.Status.DesiredNumberScheduled)
	}
	if n.checkZtunnel {
		for _, ds := range n.ztunnelDaemonSets.List() {
			expected = max(expected, ds.Status.DesiredNumberScheduled)
		}
	}
	return max(1, int(expected)*n.maxTaintedPercent/100)
}

func (n *NodeUntainter) hold(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.held.IsEmpty() {
		log.Warnf("readiness taint limit reached (%d nodes, %d%%), not tainting node %s or others until tainted nodes recover",
			n.maxTainted(), n.maxTaintedPercent, name)
	}
	n.held.Insert(name)
}

// retryHeld requeues held nodes as the budget allows. Their desired state does not change when
// another node recovers, so nothing else would requeue them.
func (n *NodeUntainter) retryHeld(stop <-chan struct{}) {
	ticker := time.NewTicker(n.heldRetry)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		budget := n.taintBudget()
		n.mu.Lock()
		for name := range n.held {
			if budget <= 0 {
				break
			}
			n.held.Delete(name)
			n.queue.Add(types.NamespacedName{Name: name})
			budget--
		}
		n.mu.Unlock()
	}
}

// anyRunsOnNode reports whether any of the DaemonSets would place a pod on the node.
func (n *NodeUntainter) anyRunsOnNode(daemonSets []*appsv1.DaemonSet, node *v1.Node) bool {
	for _, ds := range daemonSets {
		if daemonSetRunsOnNode(ds, node, n.taintName) {
			return true
		}
	}
	return false
}

// daemonSetRunsOnNode mirrors the DaemonSet controller's placement check: node selector, required
// node affinity, and NoSchedule/NoExecute taints. ignoreTaint is our own taint, which must not
// count, or adding it could make the node look like the DaemonSet does not belong there.
func daemonSetRunsOnNode(ds *appsv1.DaemonSet, node *v1.Node, ignoreTaint string) bool {
	podSpec := ds.Spec.Template.Spec
	match, err := nodeaffinity.GetRequiredNodeAffinity(&v1.Pod{Spec: podSpec}).Match(node)
	if err != nil || !match {
		return false
	}
	tolerations := append(slices.Clone(podSpec.Tolerations), daemonSetDefaultTolerations...)
	_, untolerated := corev1helpers.FindMatchingUntoleratedTaint(klog.Background(), node.Spec.Taints, tolerations, func(t *v1.Taint) bool {
		if t.Key == ignoreTaint {
			return false
		}
		return t.Effect == v1.TaintEffectNoSchedule || t.Effect == v1.TaintEffectNoExecute
	}, false)
	return !untolerated
}

func anyPodReady(pods []*v1.Pod) bool {
	for _, p := range pods {
		if IsPodReadyConditionTrue(p.Status) {
			return true
		}
	}
	return false
}

func addReadinessTaint(nodesClient kclient.Client[*v1.Node], node *v1.Node, taintName string) error {
	for _, t := range node.Spec.Taints {
		if t.Key == taintName {
			return nil
		}
	}

	log.Debugf("adding readiness taint to node %v", node.Name)
	updatedTaints := append(slices.Clone(node.Spec.Taints), v1.Taint{Key: taintName, Effect: v1.TaintEffectNoSchedule})
	// "add" replaces /spec/taints when present and creates it when the node has none,
	// so guard on the resourceVersion rather than on the taints themselves.
	atomicTaintAdd := []jsonpatch.Operation{}
	if node.ResourceVersion != "" {
		atomicTaintAdd = append(atomicTaintAdd, jsonpatch.Operation{
			Operation: "test",
			Path:      "/metadata/resourceVersion",
			Value:     node.ResourceVersion,
		})
	}
	atomicTaintAdd = append(atomicTaintAdd, jsonpatch.Operation{
		Operation: "add",
		Path:      "/spec/taints",
		Value:     updatedTaints,
	})
	patch, err := json.Marshal(atomicTaintAdd)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %v", err)
	}

	_, err = nodesClient.Patch(node.Name, "", types.JSONPatchType, patch)
	if err != nil {
		return fmt.Errorf("failed to add taint to node %v: %v", node.Name, err)
	}
	log.Debugf("added readiness taint to node %v", node.Name)
	return nil
}

func removeReadinessTaint(nodesClient kclient.Client[*v1.Node], node *v1.Node, taintName string) error {
	updatedTaint := deleteTaint(node.Spec.Taints, taintName)
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
func deleteTaint(taints []v1.Taint, taintName string) []v1.Taint {
	newTaints := []v1.Taint{}
	for i := range taints {
		if taints[i].Key == taintName {
			continue
		}
		newTaints = append(newTaints, taints[i])
	}
	return newTaints
}

func (n *NodeUntainter) hasTaint(node *v1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == n.taintName {
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
