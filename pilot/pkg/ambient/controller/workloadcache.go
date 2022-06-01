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
	"context"

	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/controllers"
)

var _ ambient.Cache = &workloadCache{}

type workloadCache struct {
	xds     model.XDSUpdater
	indexes map[ambient.NodeType]*ambient.WorkloadIndex
	pods    func(namespace string) v1.PodInterface
}

func initWorkloadCache(opts *Options) *workloadCache {
	wc := &workloadCache{
		xds:  opts.xds,
		pods: opts.Client.Kube().CoreV1().Pods,
		// 3 types of things here: uProxies, PEPs and Workloads.
		// While we don't have to look up all of these by the same keys, the indexes should be pretty cheap.
		indexes: map[ambient.NodeType]*ambient.WorkloadIndex{
			ambient.TypeUProxy:   ambient.NewWorkloadIndex(),
			ambient.TypePEP:      ambient.NewWorkloadIndex(),
			ambient.TypeWorkload: ambient.NewWorkloadIndex(),
			ambient.TypeNone:     ambient.NewWorkloadIndex(),
		},
	}
	queue := controllers.NewQueue("ambient workload cache",
		controllers.WithReconciler(wc.Reconcile),
		controllers.WithMaxAttempts(5),
	)
	proxyHandler := controllers.FilteredObjectHandler(queue.AddObject, func(o controllers.Object) bool {
		_, hasType := o.GetLabels()[ambient.LabelType]
		return hasType
	})

	opts.Client.KubeInformer().Core().V1().Pods().Informer().AddEventHandler(proxyHandler)

	go queue.Run(opts.Stop)
	return wc
}

func (wc *workloadCache) Reconcile(key types.NamespacedName) error {
	log.Infof("landow: %v", key)
	ctx := context.Background()
	// TODO use lister
	pod, err := wc.pods(key.Namespace).Get(ctx, key.Name, metav1.GetOptions{})
	if kubeErrors.IsNotFound(err) {
		wc.removeFromAll(key)
		wc.xds.ConfigUpdate(&model.PushRequest{
			// TODO scope our updates
			Full:   true,
			Reason: []model.TriggerReason{model.AmbientUpdate},
		})
		return nil
	} else if err != nil {
		return err
	}

	w := ambient.Workload{Pod: pod}
	index, ok := wc.indexes[pod.Labels[ambient.LabelType]]
	if ok && wc.validate(w) {
		// known type, cache it
		index.Insert(w)
	} else {
		// if this Pod went from valid -> empty/invalid we need to remove it from every index
		wc.removeFromAll(key)
	}
	wc.xds.ConfigUpdate(&model.PushRequest{
		// TODO scope our updates
		Full:   true,
		Reason: []model.TriggerReason{model.AmbientUpdate},
	})
	return nil
}

func (wc *workloadCache) validate(w ambient.Workload) bool {
	if w.Pod == nil {
		// should never happen
		return false
	}
	// TODO also check readiness; also requirements may differ by ambient-type
	return w.Pod.Status.PodIP != ""
}

func (wc *workloadCache) removeFromAll(key types.NamespacedName) {
	for _, index := range wc.indexes {
		index.Remove(key)
	}
}

// Workloads returns a _copy_ of the indexes in the cache, and resolves relationships between the different types.
// We copy to avoid building config from information that is only partially updated.
func (wc *workloadCache) SidecarlessWorkloads() ambient.Indexes {
	return ambient.Indexes{
		Workloads: wc.indexes[ambient.TypeWorkload].Copy(),
		None:      wc.indexes[ambient.TypeNone].Copy(),
		PEPs:      wc.indexes[ambient.TypePEP].Copy(),
		UProxies:  wc.indexes[ambient.TypeUProxy].Copy(),
	}
}
