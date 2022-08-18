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

package ambient

import (
	"encoding/json"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

// TODO shouldn't call this a workload. Maybe "node" encapsulates all of uproxy, pep, workload
type Workload struct {
	*corev1.Pod // TODO extract important things instead of embedding Pod
}

func (w Workload) Identity() string {
	// TODO duplicated to avoid import cycle
	return spiffe.MustGenSpiffeURI(w.Pod.Namespace, w.Pod.Spec.ServiceAccountName)
}

type NodeType = string

const (
	LabelStatus = "istio.io/ambient-status"
	TypeEnabled = "enabled"
	// LabelType == "workload" -> intercept into uProxy
	// TODO this could be an annotation – eventually move it into api repo
	LabelType = "ambient-type"
	// Needed for bpf code currently
	LegacyLabelType = "asm-type"

	TypeWorkload NodeType = "workload"
	TypeNone     NodeType = "none"
	TypeUProxy   NodeType = "uproxy"
	TypePEP      NodeType = "pep"

	// LabelProxy contains the name of the service account the proxy is a PEP for.
	LabelProxy = "ambient-proxy"
	// LabelPEP marks the Pod as needing a PEP (remote proxy/policy enforcement point)
	LabelPEP = "ambient-pep"
)

// Cache holds Indexes of client workloads, peps and uproxies
type Cache interface {
	SidecarlessWorkloads() Indexes
}

type Indexes struct {
	Workloads *WorkloadIndex `json:"workloads"`
	None      *WorkloadIndex `json:"none"`
	PEPs      *WorkloadIndex `json:"peps"`
	UProxies  *WorkloadIndex `json:"uproxies"`
}

type WorkloadIndex struct {
	sync.RWMutex

	ByNamespacedName  map[types.NamespacedName]Workload
	ByNode            map[string][]Workload
	ByIdentity        map[string][]Workload
	ByNodeAndIdentity map[string]map[string][]Workload
	ByIP              map[string]Workload

	// we cache this so we can cleanup the other indexes on removal without searching
	details map[types.NamespacedName]subindexDetails
}

func (wi *WorkloadIndex) MarshalJSON() ([]byte, error) {
	return json.Marshal(wi.All())
}

type subindexDetails struct {
	sa, node, ip string
}

func NewWorkloadIndex() *WorkloadIndex {
	// TODO take opts that disable individual indexes if needed
	return &WorkloadIndex{
		ByNamespacedName:  map[types.NamespacedName]Workload{},
		ByNode:            map[string][]Workload{},
		ByIdentity:        map[string][]Workload{},
		ByNodeAndIdentity: map[string]map[string][]Workload{},
		ByIP:              map[string]Workload{},
		details:           map[types.NamespacedName]subindexDetails{},
	}
}

func (wi *WorkloadIndex) NodeLocalBySA(node string) map[string][]Workload {
	if node == "" {
		return wi.ByIdentity
	}
	out := wi.ByNodeAndIdentity[node]
	if out == nil {
		out = map[string][]Workload{}
	}
	return out
}

func (wi *WorkloadIndex) NodeLocal(node string) []Workload {
	if node == "" {
		return wi.All()
	}
	return wi.ByNode[node]
}

func (wi *WorkloadIndex) All() []Workload {
	var out []Workload
	for _, workload := range wi.ByNamespacedName {
		out = append(out, workload)
	}
	return out
}

func (wi *WorkloadIndex) Insert(workload Workload) {
	if workload.Pod == nil {
		return
	}
	wi.Lock()
	defer wi.Unlock()
	node, sa := workload.Spec.NodeName, workload.Identity()
	ip := workload.Status.PodIP // TODO eventually support multi-IP
	namespacedName := types.NamespacedName{Name: workload.Name, Namespace: workload.Namespace}

	// TODO if we start indexing by a mutable key, call Remove here

	wi.ByNamespacedName[namespacedName] = workload
	wi.details[namespacedName] = subindexDetails{node: node, sa: sa, ip: ip}
	if node != "" {
		wi.ByNode[node] = append(wi.ByNode[node], workload)
	}
	if sa != "" {
		wi.ByIdentity[sa] = append(wi.ByIdentity[sa], workload)
	}
	if node != "" && sa != "" {
		if wi.ByNodeAndIdentity[node] == nil {
			wi.ByNodeAndIdentity[node] = map[string][]Workload{}
		}
		if sa != "" {
			wi.ByNodeAndIdentity[node][sa] = append(wi.ByNodeAndIdentity[node][sa], workload)
		}
	}
	if ip != "" {
		wi.ByIP[ip] = workload
	}
}

func (wi *WorkloadIndex) MergeInto(other *WorkloadIndex) *WorkloadIndex {
	wi.RLock()
	defer wi.RUnlock()
	for _, v := range wi.ByNamespacedName {
		other.Insert(v)
	}
	return other
}

func (wi *WorkloadIndex) Remove(namespacedName types.NamespacedName) {
	wi.Lock()
	defer wi.Unlock()
	details, ok := wi.details[namespacedName]
	if !ok {
		return
	}
	node, sa, ip := details.node, details.sa, details.ip
	delete(wi.ByNamespacedName, namespacedName)
	delete(wi.details, namespacedName)
	delete(wi.ByNode, node)
	delete(wi.ByNode, sa)
	delete(wi.ByIP, ip)
	if bySA, ok := wi.ByNodeAndIdentity[node]; ok {
		delete(bySA, sa)
	}
	if len(wi.ByNodeAndIdentity[node]) == 0 {
		delete(wi.ByNodeAndIdentity, node)
	}
}

func (wi *WorkloadIndex) Copy() *WorkloadIndex {
	return wi.MergeInto(NewWorkloadIndex())
}

const (
	AmbientMeshNamespace = v1alpha1.MeshConfig_AmbientMeshConfig_DEFAULT
	AmbientMeshOff       = v1alpha1.MeshConfig_AmbientMeshConfig_OFF
	AmbientMeshOn        = v1alpha1.MeshConfig_AmbientMeshConfig_ON
)

// @TODO Interim function for pep, to be replaced after design meeting
func PodHasOptOut(pod *corev1.Pod) bool {
	if val, ok := pod.Labels["ambient-type"]; ok {
		return val == "pep" || val == "none"
	}
	return false
}

func HasSelectors(lbls map[string]string, selectors []*metav1.LabelSelector) bool {
	for _, selector := range selectors {
		sel, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			log.Errorf("Failed to parse selector: %v", err)
			return false
		}

		if sel.Matches(labels.Set(lbls)) {
			return true
		}
	}
	return false
}

var LegacySelectors = []*metav1.LabelSelector{
	{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "istio-injection",
				Operator: metav1.LabelSelectorOpIn,
				Values: []string{
					"enabled",
				},
			},
		},
	},
	{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      label.IoIstioRev.Name,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	},
}

// We do not support the istio.io/rev or istio-injection sidecar labels
// If a pod or namespace has these labels, ambient mesh will not be applied
// to that namespace
func HasLegacyLabel(lbl map[string]string) bool {
	for _, ls := range LegacySelectors {
		sel, err := metav1.LabelSelectorAsSelector(ls)
		if err != nil {
			log.Errorf("Failed to parse legacy selector: %v", err)
			return false
		}

		if sel.Matches(labels.Set(lbl)) {
			return true
		}
	}

	return false
}

func hasPodIP(pod *corev1.Pod) bool {
	return pod.Status.PodIP != ""
}

func isRunning(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning
}

func ShouldPodBeInIpset(namespace *corev1.Namespace, pod *corev1.Pod, meshMode string, ignoreNotRunning bool) bool {
	// Pod must:
	// - Be running
	// - Have an IP address
	// - Ambient mesh not be off
	// - Cannot have a legacy label (istio.io/rev or istio-injection=enabled)
	// - If mesh is in namespace mode, must be in active namespace
	if (ignoreNotRunning || (isRunning(pod) && hasPodIP(pod))) &&
		meshMode != AmbientMeshOff.String() &&
		!HasLegacyLabel(pod.GetLabels()) &&
		!PodHasOptOut(pod) &&
		IsNamespaceActive(namespace, meshMode) {
		return true
	}

	return false
}

func IsNamespaceActive(namespace *corev1.Namespace, meshMode string) bool {
	// Must:
	// - MeshConfig be in an "ON" mode
	// - MeshConfig must be in a "DEFAULT" mode, plus:
	//   - Namespace cannot have "legacy" labels (ie. istio.io/rev or istio-injection=enabled)
	//   - Namespace must have label istio.io/dataplane-mode=ambient
	if meshMode == AmbientMeshOn.String() ||
		(meshMode == AmbientMeshNamespace.String() &&
			namespace != nil &&
			!HasLegacyLabel(namespace.GetLabels()) &&
			namespace.GetLabels()["istio.io/dataplane-mode"] == "ambient") {
		return true
	}

	return false
}
