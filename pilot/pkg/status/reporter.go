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

package status

import (
	"context"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/clock"

	"istio.io/istio/pilot/pkg/model"
)

func NewIstioContext(stop <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stop
		cancel()
	}()
	return ctx
}

type Reporter struct {
	mu                  sync.RWMutex
	status              map[string]string
	reverseStatus       map[string][]string
	dirty               bool
	inProgressResources []string
	client              v1.ConfigMapInterface
	cm                  *corev1.ConfigMap
	UpdateInterval      time.Duration
	PodName             string
	clock               clock.Clock
	store               model.ConfigStore
}

func (r *Reporter) Start(stop <-chan struct{}) {
	ctx := NewIstioContext(stop)
	t := r.clock.Tick(r.UpdateInterval)
	r.client = kubernetes.NewForConfigOrDie(nil).CoreV1().ConfigMaps("foo")
	go func() {
		for {
			select {
			case <-ctx.Done():
				if r.cm != nil {
					// TODO: is the use of a cancelled context here a problem?  Maybe set a short timeout context?
					if err := r.client.Delete(ctx, r.cm.Name, v12.DeleteOptions{}); err != nil {
						scope.Errorf("failed to properly clean up distribution report: %v", err)
					}
				}
				return
			case <-t:
				// TODO, check if report is necessary?  May already be handled by client
				r.writeReport(ctx)
			}
		}
	}()
}

// TODO: handle dataplane disconnect

func (r *Reporter) buildReport() (DistributionReport, []string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	finishedResources := []string{}
	out := DistributionReport{
		Reporter:       r.PodName,
		DataPlaneCount: len(r.status),
	}
	for nonce, dataplanes := range r.reverseStatus {
		for _, key := range r.inProgressResources {
			// it might be more optimal to provide for a full dump of the config at a certain version?
			res := ResourceFromString(key)
			if dpVersion, err := r.store.GetResourceAtVersion(nonce,
				model.Key(res.Resource, res.Name, res.Namespace)); err == nil {
				if dpVersion == res.ResourceVersion {
					if _, ok := out.InProgressResources[key]; !ok {
						out.InProgressResources[key] = len(dataplanes)
					} else {
						out.InProgressResources[key] += len(dataplanes)
					}
				}
			} else {
				scope.Errorf("Encountered error retrieving version %s of key %s from Store: %v", nonce, key, err)
			}
			if out.InProgressResources[key] >= out.DataPlaneCount {
				finishedResources = append(finishedResources, key)
			}
		}
	}
	return out, finishedResources
}

func (r *Reporter) removeCompletedResource(s []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []string{}
	hashtable := make(map[string]bool)
	for _, item := range s {
		hashtable[item] = true
	}
	for _, resource := range r.inProgressResources {
		if _, ok := hashtable[resource]; !ok {
			result = append(result, resource)
		}
	}
	r.inProgressResources = result
}

func (r *Reporter) AddInProgressResource(res string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inProgressResources = append(r.inProgressResources, res)
}

func (r *Reporter) writeReport(ctx context.Context) {
	report, finishedResources := r.buildReport()
	go r.removeCompletedResource(finishedResources)
	//write to kubernetes here.
	reportbytes, err := yaml.Marshal(report)
	if err != nil {
		scope.Errorf("Error serializing Distribution Report: %v", err)
		return
	}
	r.cm.Data["keyfield"] = string(reportbytes)
	// TODO: short circuit this write in the leader
	r.cm, err = r.client.Update(ctx, r.cm, v12.UpdateOptions{
		TypeMeta:     v12.TypeMeta{},
		DryRun:       nil,
		FieldManager: "",
	})
	if err != nil {
		scope.Errorf("Error writing Distribution Report: %v", err)
	}
}

func (r *Reporter) RegisterEvent(conID string, xdsType string, nonce string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = true
	// TODO might need to batch this to prevent lock contention
	key := conID + xdsType // TODO: delimit?
	if old, ok := r.status[key]; ok {
		if keys, ok := r.reverseStatus[old]; ok {
			for i := range keys {
				if keys[i] == key {
					r.reverseStatus[old] = append(keys[:i], keys[i+1:]...)
					break
				}
			}
			if len(r.reverseStatus[old]) < 1 {
				delete(r.reverseStatus, old)
			}
		}
	}
	r.status[key] = nonce
	r.reverseStatus[nonce] = append(r.reverseStatus[nonce], key)
}
