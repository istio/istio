/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package k8sresourcelock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

// Endpoints is deprecated in k8s >=1.33, but we should still support it.
// nolint: staticcheck
type EndpointsLock struct {
	// EndpointsMeta should contain a Name and a Namespace of an
	// Endpoints object that the LeaderElector will attempt to lead.
	EndpointsMeta metav1.ObjectMeta
	Client        corev1client.EndpointsGetter
	LockConfig    ResourceLockConfig
	e             *v1.Endpoints
}

// Get returns the election record from a Endpoints Annotation
func (el *EndpointsLock) Get(ctx context.Context) (*LeaderElectionRecord, []byte, error) {
	var record LeaderElectionRecord
	var err error
	el.e, err = el.Client.Endpoints(el.EndpointsMeta.Namespace).Get(ctx, el.EndpointsMeta.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	if el.e.Annotations == nil {
		el.e.Annotations = make(map[string]string)
	}
	recordStr, found := el.e.Annotations[LeaderElectionRecordAnnotationKey]
	recordBytes := []byte(recordStr)
	if found {
		if err := json.Unmarshal(recordBytes, &record); err != nil {
			return nil, nil, err
		}
	}
	return &record, recordBytes, nil
}

// Create attempts to create a LeaderElectionRecord annotation
func (el *EndpointsLock) Create(ctx context.Context, ler LeaderElectionRecord) error {
	recordBytes, err := json.Marshal(ler)
	if err != nil {
		return err
	}
	// Endpoints is deprecated in k8s >=1.33, but we should still support it.
	// nolint: staticcheck
	el.e, err = el.Client.Endpoints(el.EndpointsMeta.Namespace).Create(ctx, &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      el.EndpointsMeta.Name,
			Namespace: el.EndpointsMeta.Namespace,
			Annotations: map[string]string{
				LeaderElectionRecordAnnotationKey: string(recordBytes),
			},
		},
	}, metav1.CreateOptions{})
	return err
}

// Update will update and existing annotation on a given resource.
func (el *EndpointsLock) Update(ctx context.Context, ler LeaderElectionRecord) error {
	if el.e == nil {
		return errors.New("endpoint not initialized, call get or create first")
	}
	recordBytes, err := json.Marshal(ler)
	if err != nil {
		return err
	}
	if el.e.Annotations == nil {
		el.e.Annotations = make(map[string]string)
	}
	el.e.Annotations[LeaderElectionRecordAnnotationKey] = string(recordBytes)
	e, err := el.Client.Endpoints(el.EndpointsMeta.Namespace).Update(ctx, el.e, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	el.e = e
	return nil
}

// RecordEvent in leader election while adding meta-data
func (el *EndpointsLock) RecordEvent(s string) {
	if el.LockConfig.EventRecorder == nil {
		return
	}
	events := fmt.Sprintf("%v %v", el.LockConfig.Identity, s)
	// Endpoints is deprecated in k8s >=1.33, but we should still support it.
	// nolint: staticcheck
	el.LockConfig.EventRecorder.Eventf(&v1.Endpoints{ObjectMeta: el.e.ObjectMeta}, v1.EventTypeNormal, "LeaderElection", events)
}

// Describe is used to convert details on current resource lock
// into a string
func (el *EndpointsLock) Describe() string {
	return fmt.Sprintf("%v/%v", el.EndpointsMeta.Namespace, el.EndpointsMeta.Name)
}

// Identity returns the Identity of the lock
func (el *EndpointsLock) Identity() string {
	return el.LockConfig.Identity
}

// Key returns the Key of the lock
func (el *EndpointsLock) Key() string {
	return el.LockConfig.Key
}
