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

package leaderelection

import (
	"context"
	"time"

	"go.uber.org/atomic"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"istio.io/pkg/log"
)

// Various locks used throughout the code
const (
	NamespaceController  = "istio-namespace-controller-election"
	ValidationController = "istio-validation-controller-election"
	// This holds the legacy name to not conflict with older control plane deployments which are just
	// doing the ingress syncing.
	IngressController = "istio-leader"
	StatusController  = "istio-status-leader"
	AnalyzeController = "istio-analyze-leader"
)

type LeaderElection struct {
	namespace string
	name      string
	runFns    []func(stop <-chan struct{})
	client    kubernetes.Interface
	ttl       time.Duration

	// Records which "cycle" the election is on. This is incremented each time an election is won and then lost
	// This is mostly just for testing
	cycle      *atomic.Int32
	electionID string
}

// Run will start leader election, calling all runFns when we become the leader.
func (l *LeaderElection) Run(stop <-chan struct{}) {
	for {
		le, err := l.create()
		if err != nil {
			// This should never happen; errors are only from invalid input and the input is not user modifiable
			panic("LeaderElection creation failed: " + err.Error())
		}
		l.cycle.Inc()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-stop
			cancel()
		}()
		le.Run(ctx)
		select {
		case <-stop:
			// We were told to stop explicitly. Exit now
			return
		default:
			cancel()
			// Otherwise, we may have lost our lock. In practice, this is extremely rare; we need to have the lock, then lose it
			// Typically this means something went wrong, such as API server downtime, etc
			// If this does happen, we will start the cycle over again
			log.Errorf("Leader election cycle %v lost. Trying again", l.cycle.Load())
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
			log.Infof("leader election lock lost: %v", l.electionID)
		},
	}
	lock := resourcelock.ConfigMapLock{
		ConfigMapMeta: metaV1.ObjectMeta{Namespace: l.namespace, Name: l.electionID},
		Client:        l.client.CoreV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: l.name,
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
		ReleaseOnCancel: true,
	})
}

// AddRunFunction registers a function to run when we are the leader. These will be run asynchronously.
// To avoid running when not a leader, functions should respect the stop channel.
func (l *LeaderElection) AddRunFunction(f func(stop <-chan struct{})) *LeaderElection {
	l.runFns = append(l.runFns, f)
	return l
}

func NewLeaderElection(namespace, name, electionID string, client kubernetes.Interface) *LeaderElection {
	if name == "" {
		name = "unknown"
	}
	return &LeaderElection{
		namespace:  namespace,
		name:       name,
		electionID: electionID,
		client:     client,
		// Default to a 30s ttl. Overridable for tests
		ttl:   time.Second * 30,
		cycle: atomic.NewInt32(0),
	}
}
