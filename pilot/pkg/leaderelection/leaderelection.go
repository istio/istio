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
	"os"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"

	"istio.io/pkg/log"
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
}

// Run will start leader election, calling all runFns when we become the leader.
func (l *LeaderElection) Run(stop <-chan struct{}, onComplete func()) error {
	le, err := l.create()
	if err != nil {
		return fmt.Errorf("failed to create leader election: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		le.Run(ctx)
		// Register that we have finished leader election
		// In practice, this typically means we are exiting the process, and should drop our lock
		if onComplete != nil {
			onComplete()
		}
	}()
	go func() {
		<-stop
		cancel()
	}()
	return nil
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
	broadcaster := record.NewBroadcaster()
	hostname, _ := os.Hostname()
	recorder := broadcaster.NewRecorder(scheme.Scheme, coreV1.EventSource{
		Component: recorderComponent,
		Host:      hostname,
	})
	lock := resourcelock.ConfigMapLock{
		ConfigMapMeta: metaV1.ObjectMeta{Namespace: l.namespace, Name: electionID},
		Client:        l.client.CoreV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      l.name,
			EventRecorder: recorder,
		},
	}
	ttl := 30 * time.Second
	return leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          &lock,
		LeaseDuration: ttl,
		RenewDeadline: ttl / 2,
		RetryPeriod:   ttl / 4,
		Callbacks:     callbacks,
		// When Pilot exits, the lease will be dropped. This is more likely to lead to a case where
		// to instances are both considered the leaders. As such, if this is intended to be use for mission-critical
		// usages (rather than avoiding duplication of work), this may need to be re-evaluated.
		ReleaseOnCancel: true,
	})
}

// AddRunFunction registers a function to run when we are the leader. These will be run asynchronously.
// To avoid running when not a leader, functions should respect the stop channel.
func (l *LeaderElection) AddRunFunction(f func(stop <-chan struct{})) {
	l.runFns = append(l.runFns, f)
}

func NewLeaderElection(namespace, name string, client kubernetes.Interface) *LeaderElection {
	return &LeaderElection{namespace: namespace, name: name, client: client}
}
