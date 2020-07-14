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

package status

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/clock"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
)

func NewIstioContext(stop <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stop
		cancel()
	}()
	return ctx
}

type inProgressEntry struct {
	// the resource, including resourceVersion, we are currently tracking
	Resource
	// the number of reports we have written with this resource at 100%
	completedIterations int
}

type Reporter struct {
	mu sync.RWMutex
	// map from connection id to latest nonce
	status map[string]string
	// map from nonce to connection ids for which it is current
	// using map[string]struct to approximate a hashset
	reverseStatus          map[string]map[string]struct{}
	dirty                  bool
	inProgressResources    map[string]*inProgressEntry
	client                 v1.ConfigMapInterface
	cm                     *corev1.ConfigMap
	UpdateInterval         time.Duration
	PodName                string
	clock                  clock.Clock
	store                  model.ConfigStore
	distributionEventQueue chan distributionEvent
}

var _ xds.DistributionStatusCache = &Reporter{}

const labelKey = "internal.istio.io/distribution-report"
const dataField = "distribution-report"

// Starts the reporter, which watches dataplane ack's and resource changes so that it can update status leader
// with distribution information.  To run in read-only mode, (for supporting istioctl wait), set writeMode = false
func (r *Reporter) Start(clientSet kubernetes.Interface, namespace string, store model.ConfigStore, writeMode bool, stop <-chan struct{}) {
	scope.Info("Starting status follower controller")
	if r.clock == nil {
		r.clock = clock.RealClock{}
	}
	r.store = store
	// default UpdateInterval
	if r.UpdateInterval == 0 {
		r.UpdateInterval = 500 * time.Millisecond
	}
	r.distributionEventQueue = make(chan distributionEvent, 100_000)
	r.status = make(map[string]string)
	r.reverseStatus = make(map[string]map[string]struct{})
	r.inProgressResources = make(map[string]*inProgressEntry)
	go r.readFromEventQueue()
	if !writeMode {
		return
	}
	r.client = clientSet.CoreV1().ConfigMaps(namespace)
	r.cm = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   (r.PodName + "-distribution"),
			Labels: map[string]string{labelKey: "true"},
		},
		Data: make(map[string]string),
	}
	t := r.clock.Tick(r.UpdateInterval)
	ctx := NewIstioContext(stop)
	go func() {
		for {
			select {
			case <-ctx.Done():
				if r.cm != nil {
					// TODO: is the use of a cancelled context here a problem?  Maybe set a short timeout context?
					if err := r.client.Delete(context.Background(), r.cm.Name, metav1.DeleteOptions{}); err != nil {
						scope.Errorf("failed to properly clean up distribution report: %v", err)
					}
				}
				close(r.distributionEventQueue)
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
	// for every resource in flight
	for _, ipr := range r.inProgressResources {
		res := ipr.Resource
		key := res.String()
		// for every version (nonce) of the config currently in play
		for nonce, dataplanes := range r.reverseStatus {

			// check to see if this version of the config contains this version of the resource
			// it might be more optimal to provide for a full dump of the config at a certain version?
			dpVersion, err := r.store.GetResourceAtVersion(nonce, res.ToModelKey())
			if err == nil && dpVersion == res.ResourceVersion {
				if _, ok := out.InProgressResources[key]; !ok {
					out.InProgressResources[key] = len(dataplanes)
				} else {
					out.InProgressResources[key] += len(dataplanes)
				}
			} else if err != nil {
				scope.Errorf("Encountered error retrieving version %s of key %s from Store: %v", nonce, key, err)
				continue
			} else if nonce == r.store.Version() {
				scope.Warnf("Cache appears to be missing latest version of %s", key)
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
func (r *Reporter) removeCompletedResource(completedResources []Resource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var toDelete []Resource
	for _, item := range completedResources {
		// TODO: handle cache miss
		total := r.inProgressResources[item.ToModelKey()].completedIterations + 1
		if int64(total) > (time.Minute.Milliseconds() / r.UpdateInterval.Milliseconds()) {
			//remove from inProgressResources // TODO: cleanup completedResources
			toDelete = append(toDelete, item)
		} else {
			r.inProgressResources[item.ToModelKey()].completedIterations = total
		}
	}
	for _, resource := range toDelete {
		delete(r.inProgressResources, resource.ToModelKey())
	}
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
	r.inProgressResources[myRes.ToModelKey()] = &inProgressEntry{
		Resource:            *myRes,
		completedIterations: 0,
	}
}

func (r *Reporter) DeleteInProgressResource(res model.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inProgressResources, res.Key())
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
	_, err = CreateOrUpdateConfigMap(ctx, r.cm, r.client)
	if err != nil {
		scope.Errorf("Error writing Distribution Report: %v", err)
	}
}

// this is lifted with few modifications from kubeadm's apiclient
func CreateOrUpdateConfigMap(ctx context.Context, cm *corev1.ConfigMap, client v1.ConfigMapInterface) (res *corev1.ConfigMap, err error) {
	if res, err = client.Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) && !apierrors.IsInvalid(err) {
			scope.Errorf("%v", err)
			return nil, errors.Wrap(err, "unable to create ConfigMap")
		}

		if res, err = client.Update(context.TODO(), cm, metav1.UpdateOptions{}); err != nil {
			return nil, errors.Wrap(err, "unable to update ConfigMap")
		}
	}
	return res, nil
}

type distributionEvent struct {
	conID            string
	distributionType xds.EventType
	nonce            string
}

func (r *Reporter) QueryLastNonce(conID string, distributionType xds.EventType) (noncePrefix string) {
	key := conID + string(distributionType)
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status[key]
}

// Register that a dataplane has acknowledged a new version of the config.
// Theoretically, we could use the ads connections themselves to harvest this data,
// but the mutex there is pretty hot, and it seems best to trade memory for time.
func (r *Reporter) RegisterEvent(conID string, distributionType xds.EventType, nonce string) {
	d := distributionEvent{nonce: nonce, distributionType: distributionType, conID: conID}
	select {
	case r.distributionEventQueue <- d:
		return
	default:
		scope.Errorf("Distribution Event Queue overwhelmed, status will be invalid.")
	}
}

func (r *Reporter) readFromEventQueue() {
	for ev := range r.distributionEventQueue {
		// TODO might need to batch this to prevent lock contention
		r.processEvent(ev.conID, ev.distributionType, ev.nonce)
	}

}
func (r *Reporter) processEvent(conID string, distributionType xds.EventType, nonce string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = true
	key := conID + string(distributionType) // TODO: delimit?
	r.deleteKeyFromReverseMap(key)
	var version string
	if len(nonce) > 12 {
		version = nonce[:xds.VersionLen]
	} else {
		version = nonce
	}
	// touch
	r.status[key] = version
	if _, ok := r.reverseStatus[version]; !ok {
		r.reverseStatus[version] = make(map[string]struct{})
	}
	r.reverseStatus[version][key] = struct{}{}
}

// This is a helper function for keeping our reverseStatus map in step with status.
// must have write lock before calling.
func (r *Reporter) deleteKeyFromReverseMap(key string) {
	if old, ok := r.status[key]; ok {
		if keys, ok := r.reverseStatus[old]; ok {
			delete(keys, key)
			if len(r.reverseStatus[old]) < 1 {
				delete(r.reverseStatus, old)
			}
		}
	}
}

// When a dataplane disconnects, we should no longer count it, nor expect it to ack config.
func (r *Reporter) RegisterDisconnect(conID string, types []xds.EventType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirty = true
	for _, xdsType := range types {
		key := conID + string(xdsType) // TODO: delimit?
		r.deleteKeyFromReverseMap(key)
		delete(r.status, key)
	}
}
