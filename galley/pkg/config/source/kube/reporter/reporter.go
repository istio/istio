// Copyright 2019 Istio Authors
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

package reporter

import (
	"sync"

	"istio.io/pkg/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/rt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var scope = log.RegisterScope("reporter", "", 0)

type key struct {
	kind       string
	name       string
	namespace  string
	apiVersion string
}

// Instance is a new diagnostic reporter instance for Kubernetes
type Instance struct {
	mu sync.Mutex

	reporters map[string]*reporter

	cur map[key]*unstructured.Unstructured

	stopCh chan struct{}
}

var _ processing.StatusReporter = &Instance{}

// New returns a new Reporter
func New(k kube.Interfaces, resources schema.KubeResources) (*Instance, error) {
	d, err := k.DynamicInterface()
	if err != nil {
		return nil, err
	}

	i := &Instance{
		reporters: make(map[string]*reporter),
		cur:       make(map[key]*unstructured.Unstructured),
	}

	for _, r := range resources {
		nri := d.Resource(kubeSchema.GroupVersionResource{})
		rep := &reporter{
			nri:      nri,
			resource: r,
		}
		i.reporters[r.Kind] = rep
	}

	return i, nil
}

// Report implements processing.StatusReporter
func (r *Instance) Report(msgs diag.Messages) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur := r.cur
	cur = r.calculateNewCur(msgs, cur)
	if r.stopCh != nil {
		close(r.stopCh)
	}
	r.stopCh = make(chan struct{})

	go r.update(r.stopCh, cur)
}

func (r *Instance) calculateNewCur(msgs diag.Messages, prev map[key]*unstructured.Unstructured) map[key]*unstructured.Unstructured {
	m := make(map[key]*unstructured.Unstructured)

	for _, msg := range msgs {
		k, s := r.toStatus(msg)
		m[k] = s
	}

	for k := range prev {
		if _, contains := m[k]; !contains { // TODO: Avoid updating status if it is already empty
			k, s := r.toEmptyStatus(k)
			m[k] = s
		}
	}

	return m
}

func (r *Instance) toStatus(msg diag.Message) (key, *unstructured.Unstructured) {
	o := msg.Origin.(*rt.Origin)

	u := &unstructured.Unstructured{}
	u.SetName(o.Name)
	u.SetNamespace(o.Namespace)
	u.SetKind(o.GVR.Resource)
	u.SetAPIVersion(o.GVR.GroupVersion().String())
	return key{
		name:       o.Name,
		namespace:  o.Namespace,
		kind:       o.GVR.Resource,
		apiVersion: o.GVR.GroupVersion().String(),
	}, u

}

func (r *Instance) toEmptyStatus(k key) (key, *unstructured.Unstructured) {
	u := &unstructured.Unstructured{}
	u.SetName(k.name)
	u.SetNamespace(k.namespace)
	u.SetKind(k.kind)
	u.SetAPIVersion(k.apiVersion)
	return k, u
}

func (r *Instance) update(stopCh chan struct{}, set map[key]*unstructured.Unstructured) {
loop:
	for k, v := range set {
		select {
		case <-stopCh:
			break loop
		default:
		}

		r.reporters[k.kind].report(v)
	}
}

type reporter struct {
	resource schema.KubeResource
	nri      dynamic.NamespaceableResourceInterface
}

func (r *reporter) report(u *unstructured.Unstructured) {
	_, err := r.nri.UpdateStatus(u, metav1.UpdateOptions{})
	if err != nil {
		scope.Errorf("Error updating status for %s/%s: %v", u.GetNamespace(), u.GetName(), err)
	}
}
