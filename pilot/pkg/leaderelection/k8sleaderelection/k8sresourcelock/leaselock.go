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

package k8sresourcelock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coordinationv1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
)

type LeaseLock struct {
	// LeaseMeta should contain a Name and a Namespace of a
	// LeaseMeta object that the LeaderElector will attempt to lead.
	LeaseMeta  metav1.ObjectMeta
	Client     coordinationv1client.LeasesGetter
	LockConfig ResourceLockConfig
	lease      *coordinationv1.Lease
}

// Get returns the election record from a Lease spec
func (ll *LeaseLock) Get(ctx context.Context) (*LeaderElectionRecord, []byte, error) {
	var err error
	ll.lease, err = ll.Client.Leases(ll.LeaseMeta.Namespace).Get(ctx, ll.LeaseMeta.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	record := LeaseToLeaderElectionRecord(ll.lease)
	recordByte, err := json.Marshal(*record)
	if err != nil {
		return nil, nil, err
	}
	return record, recordByte, nil
}

// Create attempts to create a Lease
func (ll *LeaseLock) Create(ctx context.Context, ler LeaderElectionRecord) error {
	var err error
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ll.LeaseMeta.Name,
			Namespace: ll.LeaseMeta.Namespace,
		},
		Spec: LeaderElectionRecordToLeaseSpec(&ler),
	}
	ensureHolderKey(ler, lease)
	ll.lease, err = ll.Client.Leases(ll.LeaseMeta.Namespace).Create(ctx, lease, metav1.CreateOptions{})
	return err
}

func ensureHolderKey(ler LeaderElectionRecord, lease *coordinationv1.Lease) {
	if ler.HolderKey != "" {
		if lease.Annotations == nil {
			lease.Annotations = make(map[string]string)
		}
		lease.Annotations[LeaseHolderKey] = ler.HolderKey
	} else {
		delete(lease.Annotations, LeaseHolderKey)
	}
}

const LeaseHolderKey = "leader.istio.io/holder-key"

// Update will update an existing Lease spec.
func (ll *LeaseLock) Update(ctx context.Context, ler LeaderElectionRecord) error {
	if ll.lease == nil {
		return errors.New("lease not initialized, call get or create first")
	}
	ll.lease.Spec = LeaderElectionRecordToLeaseSpec(&ler)
	ensureHolderKey(ler, ll.lease)

	lease, err := ll.Client.Leases(ll.LeaseMeta.Namespace).Update(ctx, ll.lease, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	ll.lease = lease
	return nil
}

// RecordEvent in leader election while adding meta-data
func (ll *LeaseLock) RecordEvent(s string) {
	if ll.LockConfig.EventRecorder == nil {
		return
	}
	events := fmt.Sprintf("%v %v", ll.LockConfig.Identity, s)
	ll.LockConfig.EventRecorder.Eventf(&coordinationv1.Lease{ObjectMeta: ll.lease.ObjectMeta}, corev1.EventTypeNormal, "LeaderElection", events)
}

// Describe is used to convert details on current resource lock
// into a string
func (ll *LeaseLock) Describe() string {
	return fmt.Sprintf("%v/%v", ll.LeaseMeta.Namespace, ll.LeaseMeta.Name)
}

// Identity returns the Identity of the lock
func (ll *LeaseLock) Identity() string {
	return ll.LockConfig.Identity
}

// Key returns the Key of the lock
func (ll *LeaseLock) Key() string {
	return ll.LockConfig.Key
}

func LeaseToLeaderElectionRecord(l *coordinationv1.Lease) *LeaderElectionRecord {
	spec := l.Spec
	var r LeaderElectionRecord
	if spec.HolderIdentity != nil {
		r.HolderIdentity = *spec.HolderIdentity
	}
	if spec.LeaseDurationSeconds != nil {
		r.LeaseDurationSeconds = int(*spec.LeaseDurationSeconds)
	}
	if spec.LeaseTransitions != nil {
		r.LeaderTransitions = int(*spec.LeaseTransitions)
	}
	if spec.AcquireTime != nil {
		r.AcquireTime = metav1.Time{Time: spec.AcquireTime.Time}
	}
	if spec.RenewTime != nil {
		r.RenewTime = metav1.Time{Time: spec.RenewTime.Time}
	}
	r.HolderKey = l.Annotations[LeaseHolderKey]

	return &r
}

func LeaderElectionRecordToLeaseSpec(ler *LeaderElectionRecord) coordinationv1.LeaseSpec {
	leaseDurationSeconds := int32(ler.LeaseDurationSeconds)
	leaseTransitions := int32(ler.LeaderTransitions)
	return coordinationv1.LeaseSpec{
		HolderIdentity:       &ler.HolderIdentity,
		LeaseDurationSeconds: &leaseDurationSeconds,
		AcquireTime:          &metav1.MicroTime{Time: ler.AcquireTime.Time},
		RenewTime:            &metav1.MicroTime{Time: ler.RenewTime.Time},
		LeaseTransitions:     &leaseTransitions,
	}
}
