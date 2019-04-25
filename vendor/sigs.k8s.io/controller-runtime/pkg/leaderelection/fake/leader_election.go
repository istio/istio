/*
Copyright 2018 The Kubernetes Authors.

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

package fake

import (
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime/pkg/leaderelection"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
)

// NewResourceLock creates a new ResourceLock for use in testing
// leader election.
func NewResourceLock(config *rest.Config, recorderProvider recorder.Provider, options leaderelection.Options) (resourcelock.Interface, error) {
	// Leader id, needs to be unique
	id, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	id = id + "_" + string(uuid.NewUUID())

	return &ResourceLock{
		id: id,
		record: resourcelock.LeaderElectionRecord{
			HolderIdentity:       id,
			LeaseDurationSeconds: 15,
			AcquireTime:          metav1.NewTime(time.Now()),
			RenewTime:            metav1.NewTime(time.Now().Add(15 * time.Second)),
			LeaderTransitions:    1,
		},
	}, nil
}

// ResourceLock implements the ResourceLockInterface.
// By default returns that the current identity holds the lock.
type ResourceLock struct {
	id     string
	record resourcelock.LeaderElectionRecord
}

// Get implements the ResourceLockInterface.
func (f *ResourceLock) Get() (*resourcelock.LeaderElectionRecord, error) {
	return &f.record, nil
}

// Create implements the ResourceLockInterface.
func (f *ResourceLock) Create(ler resourcelock.LeaderElectionRecord) error {
	f.record = ler
	return nil
}

// Update implements the ResourceLockInterface.
func (f *ResourceLock) Update(ler resourcelock.LeaderElectionRecord) error {
	f.record = ler
	return nil
}

// RecordEvent implements the ResourceLockInterface.
func (f *ResourceLock) RecordEvent(s string) {
	return
}

// Identity implements the ResourceLockInterface.
func (f *ResourceLock) Identity() string {
	return f.id
}

// Describe implements the ResourceLockInterface.
func (f *ResourceLock) Describe() string {
	return f.id
}
