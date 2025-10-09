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

package nodeagent

import (
	"context"
	"fmt"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/pkg/kube"
)

const defaultZTunnelKeepAliveCheckInterval = 5 * time.Second

var (
	log              = scopes.CNIAgent
	tokenWaitBackoff = time.Second
)

type MeshDataplane interface {
	// MUST be called first, (even before Start()).
	ConstructInitialSnapshot(existingAmbientPods []*corev1.Pod) error
	Start(ctx context.Context)

	AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error
	RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error

	Stop(skipCleanup bool)
}

type Server struct {
	ctx        context.Context
	kubeClient kube.Client

	handlers  K8sHandlers
	dataplane MeshDataplane

	isReady *atomic.Value

	cniServerStopFunc func()
}

func NewServer(ctx context.Context, ready *atomic.Value, pluginSocket string, args AmbientArgs) (*Server, error) {
	client, err := buildKubeClient(args.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing kube client: %w", err)
	}

	s := &Server{
		ctx:        ctx,
		kubeClient: client,
		isReady:    ready,
	}

	s.dataplane, err = initMeshDataplane(client, args)
	if err != nil {
		return nil, fmt.Errorf("error initializing mesh dataplane: %w", err)
	}

	s.NotReady()
	s.handlers = setupHandlers(s.ctx, s.kubeClient, s.dataplane, args.SystemNamespace, args.EnablementSelector)

	cniServer := startCniPluginServer(ctx, pluginSocket, s.handlers, s.dataplane)
	err = cniServer.Start()
	if err != nil {
		return nil, fmt.Errorf("error starting cni server: %w", err)
	}
	s.cniServerStopFunc = cniServer.Stop

	return s, nil
}

func (s *Server) Ready() {
	s.isReady.Store(true)
}

func (s *Server) NotReady() {
	s.isReady.Store(false)
}

func (s *Server) Start() {
	log.Info("CNI ambient server starting")
	s.kubeClient.RunAndWait(s.ctx.Done())
	log.Info("CNI ambient server kubeclient started")
	pods := s.handlers.GetActiveAmbientPodSnapshot()
	err := s.dataplane.ConstructInitialSnapshot(pods)
	// Start the informer handlers FIRST, before we snapshot.
	// They will keep the (mutex'd) snapshot cache synced.
	s.handlers.Start()
	if err != nil {
		log.Warnf("failed to construct initial snapshot: %v", err)
	}
	// Start accepting ztunnel connections
	// (and send current snapshot when we get one)
	s.dataplane.Start(s.ctx)
	// Everything (informer handlers, snapshot, zt server) ready to go
	log.Info("CNI ambient server marking ready")
	s.Ready()
}

func (s *Server) Stop(skipCleanup bool) {
	s.cniServerStopFunc()
	s.dataplane.Stop(skipCleanup)
}

// ShouldStopCleanup of istio-cni config and binary when upgrading or on node reboot
func (s *Server) ShouldStopCleanup(selfName, selfNamespace string, istioOwnedCNIConfig bool) bool {
	dsName := fmt.Sprintf("%s-node", selfName)
	shouldStopCleanup := false
	var numRetries uint64
	// use different defaults when using an istio owned CNI config file
	if istioOwnedCNIConfig {
		shouldStopCleanup = true
		numRetries = 2
	}
	err := backoff.Retry(
		func() error {
			cniDS, err := s.kubeClient.Kube().AppsV1().DaemonSets(selfNamespace).Get(context.Background(), dsName, metav1.GetOptions{})

			if err == nil && cniDS != nil && cniDS.DeletionTimestamp == nil {
				log.Infof("terminating, but parent DaemonSet %s is still present, this is an upgrade or a node reboot, leaving plugin in place", dsName)
				shouldStopCleanup = true
				return nil
			}
			if errors.IsNotFound(err) || (cniDS != nil && cniDS.DeletionTimestamp != nil) {
				// If the DS is gone, or marked for deletion, this is not an upgrade.
				// We can safely shut down the plugin.
				log.Infof("parent DaemonSet %s is not found or marked for deletion, this is not an upgrade, shutting down normally", dsName)
				shouldStopCleanup = false
				return nil
			}
			if errors.IsUnauthorized(err) {
				log.Infof("permission to get parent DaemonSet %s has been revoked manually or due to uninstall, this is not an upgrade, "+
					"shutting down normally", dsName)
				shouldStopCleanup = false
				return nil
			}
			log.Infof("failed to get parent DS %s, retrying: %v", dsName, err)
			return err
		},
		// Limiting retries to 3 so other shutdown tasks can complete before the graceful shutdown period ends
		backoff.WithMaxRetries(backoff.NewConstantBackOff(tokenWaitBackoff), numRetries))
	if err != nil {
		log.Infof("failed to get parent DaemonSet %s, returning %t: %v", dsName, shouldStopCleanup, err)
	}
	return shouldStopCleanup
}

// buildKubeClient creates the kube client
func buildKubeClient(kubeConfig string) (kube.Client, error) {
	// Used by validation
	kubeRestConfig, err := kube.DefaultRestConfig(kubeConfig, "", func(config *rest.Config) {
		config.QPS = 80
		config.Burst = 160
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating kube config: %v", err)
	}

	client, err := kube.NewClient(kube.NewClientConfigForRestConfig(kubeRestConfig), "")
	if err != nil {
		return nil, fmt.Errorf("failed creating kube client: %v", err)
	}

	return client, nil
}
