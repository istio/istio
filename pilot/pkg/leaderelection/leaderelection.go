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
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/atomic"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection/k8sleaderelection"
	"istio.io/istio/pilot/pkg/leaderelection/k8sleaderelection/k8sresourcelock"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/revisions"
	"istio.io/pkg/log"
)

// Various locks used throughout the code
const (
	NamespaceController     = "istio-namespace-controller-election"
	ServiceExportController = "istio-serviceexport-controller-election"
	// This holds the legacy name to not conflict with older control plane deployments which are just
	// doing the ingress syncing.
	IngressController = "istio-leader"
	// GatewayStatusController controls the status of gateway.networking.k8s.io objects. For the v1alpha1
	// this was formally "istio-gateway-leader"; because they are a different API group we need a different
	// election to ensure we do not only handle one or the other.
	GatewayStatusController = "istio-gateway-status-leader"
	// GatewayDeploymentController controls the Deployment/Service generation from Gateways. This is
	// separate from GatewayStatusController to allow running in a separate process (for low priv).
	GatewayDeploymentController = "istio-gateway-deployment-leader"
	StatusController            = "istio-status-leader"
	AnalyzeController           = "istio-analyze-leader"
)

// Leader election key prefix for remote istiod managed clusters
const remoteIstiodPrefix = "^"

type LeaderElection struct {
	namespace string
	name      string
	runFns    []func(stop <-chan struct{})
	client    kubernetes.Interface
	ttl       time.Duration

	// Criteria to determine leader priority.
	revision       string
	remote         bool
	prioritized    bool
	defaultWatcher revisions.DefaultWatcher

	// Records which "cycle" the election is on. This is incremented each time an election is won and then lost
	// This is mostly just for testing
	cycle      *atomic.Int32
	electionID string

	// Store as field for testing
	le *k8sleaderelection.LeaderElector
	mu sync.RWMutex
}

// Run will start leader election, calling all runFns when we become the leader.
func (l *LeaderElection) Run(stop <-chan struct{}) {
	if l.prioritized && l.defaultWatcher != nil {
		go l.defaultWatcher.Run(stop)
	}
	for {
		le, err := l.create()
		if err != nil {
			// This should never happen; errors are only from invalid input and the input is not user modifiable
			panic("LeaderElection creation failed: " + err.Error())
		}
		l.mu.Lock()
		l.le = le
		l.cycle.Inc()
		l.mu.Unlock()
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
			// Otherwise, we may have lost our lock. This can happen when the default revision changes and steals
			// the lock from us.
			log.Infof("Leader election cycle %v lost. Trying again", l.cycle.Load())
		}
	}
}

func (l *LeaderElection) create() (*k8sleaderelection.LeaderElector, error) {
	callbacks := k8sleaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			log.Infof("leader election lock obtained: %v", l.electionID)
			for _, f := range l.runFns {
				go f(ctx.Done())
			}
		},
		OnStoppedLeading: func() {
			log.Infof("leader election lock lost: %v", l.electionID)
		},
	}

	key := l.revision
	if l.remote {
		key = remoteIstiodPrefix + key
	}
	lock := k8sresourcelock.ConfigMapLock{
		ConfigMapMeta: metaV1.ObjectMeta{Namespace: l.namespace, Name: l.electionID},
		Client:        l.client.CoreV1(),
		LockConfig: k8sresourcelock.ResourceLockConfig{
			Identity: l.name,
			Key:      key,
		},
	}

	config := k8sleaderelection.LeaderElectionConfig{
		Lock:          &lock,
		LeaseDuration: l.ttl,
		RenewDeadline: l.ttl / 2,
		RetryPeriod:   l.ttl / 4,
		Callbacks:     callbacks,
		// When Pilot exits, the lease will be dropped. This is more likely to lead to a case where
		// to instances are both considered the leaders. As such, if this is intended to be use for mission-critical
		// usages (rather than avoiding duplication of work), this may need to be re-evaluated.
		ReleaseOnCancel: true,
	}

	if l.prioritized {
		// Function to use to decide whether this leader should steal the existing lock.
		config.KeyComparison = func(leaderKey string) bool {
			return LocationPrioritizedComparison(leaderKey, l)
		}
	}

	return k8sleaderelection.NewLeaderElector(config)
}

func LocationPrioritizedComparison(currentLeaderRevision string, l *LeaderElection) bool {
	var currentLeaderRemote bool
	if currentLeaderRemote = strings.HasPrefix(currentLeaderRevision, remoteIstiodPrefix); currentLeaderRemote {
		currentLeaderRevision = strings.TrimPrefix(currentLeaderRevision, remoteIstiodPrefix)
	}
	defaultRevision := l.defaultWatcher.GetDefault()
	if l.revision != currentLeaderRevision && defaultRevision != "" && defaultRevision == l.revision {
		// Always steal the lock if the new one is the default revision and the current one is not
		return true
	}
	// Otherwise steal the lock if the new one and the current one are the same revision, but new one is local and current is remote
	return l.revision == currentLeaderRevision && !l.remote && currentLeaderRemote
}

// AddRunFunction registers a function to run when we are the leader. These will be run asynchronously.
// To avoid running when not a leader, functions should respect the stop channel.
func (l *LeaderElection) AddRunFunction(f func(stop <-chan struct{})) *LeaderElection {
	l.runFns = append(l.runFns, f)
	return l
}

func NewLeaderElection(namespace, name, electionID, revision string, client kube.Client) *LeaderElection {
	return NewLeaderElectionMulticluster(namespace, name, electionID, revision, false, client)
}

func NewLeaderElectionMulticluster(namespace, name, electionID, revision string, remote bool, client kube.Client) *LeaderElection {
	var watcher revisions.DefaultWatcher
	if features.PrioritizedLeaderElection {
		watcher = revisions.NewDefaultWatcher(client, revision)
	}
	if name == "" {
		hn, _ := os.Hostname()
		name = fmt.Sprintf("unknown-%s", hn)
	}
	return &LeaderElection{
		namespace:      namespace,
		name:           name,
		client:         client.Kube(),
		electionID:     electionID,
		revision:       revision,
		remote:         remote,
		prioritized:    features.PrioritizedLeaderElection,
		defaultWatcher: watcher,
		// Default to a 30s ttl. Overridable for tests
		ttl:   time.Second * 30,
		cycle: atomic.NewInt32(0),
		mu:    sync.RWMutex{},
	}
}

func (l *LeaderElection) isLeader() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.le == nil {
		return false
	}
	return l.le.IsLeader()
}
