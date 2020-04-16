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

	"k8s.io/client-go/rest"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	inProgressResources []Resource
	client              v1.ConfigMapInterface
	cm                  *corev1.ConfigMap
	UpdateInterval      time.Duration
	PodName             string
	clock               clock.Clock
	store               model.ConfigStore
}

const labelKey = "internal.istio.io/distribution-report"
const dataField = "distribution-report"

// Starts the reporter, which watches dataplane ack's and resource changes so that it can update status leader
// with distribution information
func (r *Reporter) Start(restConfig *rest.Config, namespace string, stop <-chan struct{}) {
	ctx := NewIstioContext(stop)
	if r.clock == nil {
		r.clock = clock.RealClock{}
	}
	// default UpdateInterval
	if r.UpdateInterval == 0 {
		r.UpdateInterval = 500 * time.Millisecond
	}
	t := r.clock.Tick(r.UpdateInterval)
	clientSet, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		scope.Fatalf("Could not connect to kubernetes: %s", err)
	}
	r.client = clientSet.CoreV1().ConfigMaps(namespace)
	r.cm = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   (r.PodName + "-distribution"),
			Labels: map[string]string{labelKey: "true"},
		},
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				if r.cm != nil {
					// TODO: is the use of a cancelled context here a problem?  Maybe set a short timeout context?
					if err := r.client.Delete(ctx, r.cm.Name, metav1.DeleteOptions{}); err != nil {
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

// build a distribution report to send to status leader
func (r *Reporter) buildReport() (DistributionReport, []Resource) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var finishedResources []Resource
	out := DistributionReport{
		Reporter:            r.PodName,
		DataPlaneCount:      len(r.status),
		InProgressResources: map[string]int{},
	}
	// for every version (nonce) of the config currently in play
	for nonce, dataplanes := range r.reverseStatus {
		// for every resource in flight
		for _, res := range r.inProgressResources {
			// check to see if this version of the config contains this version of the resource
			// it might be more optimal to provide for a full dump of the config at a certain version?
			key := res.String()
			if dpVersion, err := r.store.GetResourceAtVersion(nonce,
				res.ToModelKey()); err == nil && dpVersion == res.ResourceVersion {
				if _, ok := out.InProgressResources[key]; !ok {
					out.InProgressResources[key] = len(dataplanes)
				} else {
					out.InProgressResources[key] += len(dataplanes)
				}
			} else if err != nil {
				scope.Errorf("Encountered error retrieving version %s of key %s from Store: %v", nonce, key, err)
			}
			if out.InProgressResources[key] >= out.DataPlaneCount {
				// if this resource is done reconciling, let's not worry about it anymore
				finishedResources = append(finishedResources, res)
				// deleting it here doesn't work because we have a read lock and are inside an iterator.
				// TODO: this will leak when a resource never reaches 100% before it is replaced.
				// TODO: do deletes propagate through this thing?
			}
		}
	}
	return out, finishedResources
}

// For efficiency, we don't want to be checking on resources that have already reached 100% distribution.
// When this happens, we remove them from our watch list.
func (r *Reporter) removeCompletedResource(s []Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []Resource
	hashtable := make(map[Resource]bool)
	for _, item := range s {
		hashtable[item] = true
	}
	for _, ipr := range r.inProgressResources {
		if _, ok := hashtable[ipr]; !ok {
			result = append(result, ipr)
		}
	}
	r.inProgressResources = result
}

// This function must be called every time a resource change is detected by pilot.  This allows us to lookup
// only the resources we expect to be in flight, not the ones that have already distributed
func (r *Reporter) AddInProgressResource(res model.Config) {
	myRes := ResourceFromModelConfig(res)
	if myRes == nil {
		scope.Errorf("Unable to locate schema for %v, will not update status.", res)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inProgressResources = append(r.inProgressResources, *myRes)
}

// generate a distribution report and write it to a ConfigMap for the leader to read.
func (r *Reporter) writeReport(ctx context.Context) {
	report, finishedResources := r.buildReport()
	go r.removeCompletedResource(finishedResources)
	//write to kubernetes here.
	reportbytes, err := yaml.Marshal(report)
	if err != nil {
		scope.Errorf("Error serializing Distribution Report: %v", err)
		return
	}
	r.cm.Data[dataField] = string(reportbytes)
	// TODO: short circuit this write in the leader
	r.cm, err = r.client.Update(ctx, r.cm, metav1.UpdateOptions{
		TypeMeta:     metav1.TypeMeta{},
		DryRun:       nil,
		FieldManager: "",
	})
	if err != nil {
		scope.Errorf("Error writing Distribution Report: %v", err)
	}
}

// Register that a dataplane has acknowledged a new version of the config.
// Theoretically, we could use the ads connections themselves to harvest this data,
// but the mutex there is pretty hot, and it seems best to trade memory for time.
func (r *Reporter) RegisterEvent(conID string, xdsType string, nonce string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = true
	// TODO might need to batch this to prevent lock contention
	key := conID + xdsType // TODO: delimit?
	r.deleteKeyFromReverseMap(key)
	r.status[key] = nonce
	r.reverseStatus[nonce] = append(r.reverseStatus[nonce], key)
}

// This is a helper function for keeping our reverseStatus map in step with status.
// must have write lock before calling.
func (r *Reporter) deleteKeyFromReverseMap(key string) {
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
}

// When a dataplane disconnects, we should no longer count it, nor expect it to ack config.
func (r *Reporter) RegisterDisconnect(conID string, xdsTypes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = true
	for _, xdsType := range xdsTypes {
		key := conID + xdsType // TODO: delimit?
		r.deleteKeyFromReverseMap(key)
		delete(r.status, key)
	}
}
