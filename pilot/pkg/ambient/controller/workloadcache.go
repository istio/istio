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
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/client-go/listers/core/v1"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/ambient/ambientpod"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/controllers"
)

var _ ambient.Cache = &workloadCache{}

type workloadCache struct {
	xds       model.XDSUpdater
	indexes   map[ambient.NodeType]*ambient.WorkloadIndex
	podLister v1.PodLister
}

func initWorkloadCache(opts Options) *workloadCache {
	wc := &workloadCache{
		xds:       opts.xds,
		podLister: opts.Client.KubeInformer().Core().V1().Pods().Lister(),
		// 3 types of things here: ztunnels, waypoint proxies, and Workloads.
		// While we don't have to look up all of these by the same keys, the indexes should be pretty cheap.
		indexes: map[ambient.NodeType]*ambient.WorkloadIndex{
			ambient.TypeZTunnel:  ambient.NewWorkloadIndex(),
			ambient.TypeWaypoint: ambient.NewWorkloadIndex(),
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
	_, _ = opts.Client.KubeInformer().Core().V1().Pods().Informer().AddEventHandler(proxyHandler)

	go queue.Run(opts.Stop)
	return wc
}

func (wc *workloadCache) Reconcile(key types.NamespacedName) error {
	pod, err := wc.podLister.Pods(key.Namespace).Get(key.Name)
	if err != nil {
		if kubeErrors.IsNotFound(err) {
			log.Debugf("trigger full push on delete pod %s", key)
			wc.removeFromAll(key)
			wc.triggerPush(key)
			return nil
		}
		return err
	}

	w := ambientpod.WorkloadFromPod(pod)
	index, ok := wc.indexes[pod.Labels[ambient.LabelType]]
	if ok && wc.validate(w) {
		cur := index.ByNamespacedName[key]
		if !cur.Equals(w) {
			// known type, cache it
			index.Insert(w)
			log.Debugf("trigger full push on update pod %s", key)
			wc.triggerPush(key)
		}
	} else {
		// if this Pod went from valid -> empty/invalid we need to remove it from every index
		wc.removeFromAll(key)
		wc.triggerPush(key)
	}

	return nil
}

func (wc *workloadCache) validate(w ambient.Workload) bool {
	// TODO also check readiness; also requirements may differ by ambient-type
	return w.PodIP != ""
}

func (wc *workloadCache) removeFromAll(key types.NamespacedName) {
	for _, index := range wc.indexes {
		index.Remove(key)
	}
}

// AmbientWorkloads returns a _copy_ of the indexes in the cache, and resolves relationships between the different
// types. We copy to avoid building config from information that is only partially updated.
func (wc *workloadCache) AmbientWorkloads() ambient.Indexes {
	return ambient.Indexes{
		Workloads: wc.indexes[ambient.TypeWorkload].Copy(),
		None:      wc.indexes[ambient.TypeNone].Copy(),
		Waypoints: wc.indexes[ambient.TypeWaypoint].Copy(),
		ZTunnels:  wc.indexes[ambient.TypeZTunnel].Copy(),
	}
}

func (wc *workloadCache) triggerPush(key types.NamespacedName) {
	wc.xds.ConfigUpdate(&model.PushRequest{
		Full: true, // TODO: find a better way?
		ConfigsUpdated: map[model.ConfigKey]struct{}{{
			Kind:      kind.Pod,
			Name:      key.Name,
			Namespace: key.Namespace,
		}: {}},
		Reason: []model.TriggerReason{model.AmbientUpdate},
	})
}
