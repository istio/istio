// Copyright 2020 Istio Authors
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

package leaderelection

import (
	"context"
	"fmt"
	"time"

	"istio.io/pkg/log"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	electionID        = "istio-leader"
	recorderComponent = "istio-leader-elector"
)

type LeaderElection struct {
	namespace string
	name      string
	runFns    []func(stop <-chan struct{})
	client    kubernetes.Interface
	le        *leaderelection.LeaderElector
	ttl       time.Duration
}

// Run will start leader election, calling all runFns when we become the leader.
func (l *LeaderElection) Run(stop <-chan struct{}) {
	if l.le == nil {
		panic("Build() must be called before Run()")
	}
	for {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-stop
			cancel()
		}()
		l.le.Run(ctx)
		select {
		case <-stop:
			// We were told to stop explicitly. Exit now
			return
		default:
			log.Errorf("Leader election lost. Trying again")
			// Otherwise, we may have lost our lock. In practice, this is extremely rare; we need to have the lock, then lose it
			// Typically this means something went wrong, such as API server downtime, etc
			// If this does happen, we will start the cycle over again
		}
	}
}

func (l *LeaderElection) create() (*leaderelection.LeaderElector, error) {
	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			for _, f := range l.runFns {
				go f(ctx.Done())
			}
		},
		OnStoppedLeading: func() {
			log.Infof("leader election lock lost")

		},
	}
	lock := resourcelock.ConfigMapLock{
		ConfigMapMeta: metaV1.ObjectMeta{Namespace: l.namespace, Name: electionID},
		Client:        l.client.CoreV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      l.name,
		},
	}
	return leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          &lock,
		LeaseDuration: l.ttl,
		RenewDeadline: l.ttl / 2,
		RetryPeriod:   l.ttl / 4,
		Callbacks:     callbacks,
		// When Pilot exits, the lease will be dropped. This is more likely to lead to a case where
		// to instances are both considered the leaders. As such, if this is intended to be use for mission-critical
		// usages (rather than avoiding duplication of work), this may need to be re-evaluated.
		// TODO this should be true once https://github.com/kubernetes/kubernetes/issues/87800 is fixed
		ReleaseOnCancel: true,
	})
}

// AddRunFunction registers a function to run when we are the leader. These will be run asynchronously.
// To avoid running when not a leader, functions should respect the stop channel.
func (l *LeaderElection) AddRunFunction(f func(stop <-chan struct{})) {
	l.runFns = append(l.runFns, f)
}

func (l *LeaderElection) Build() error {
	le, err := l.create()
	if err != nil {
		return fmt.Errorf("failed to create leader election: %v", err)
	}
	l.le = le
	return nil
}

func NewLeaderElection(namespace, name string, client kubernetes.Interface) *LeaderElection {
	return &LeaderElection{
		namespace: namespace,
		name:      name,
		client:    client,
		// Default to a 30s ttl. Overridable for tests
		ttl: time.Second * 30,
	}
}
