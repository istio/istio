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
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	pconstants "istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/nodeagent/constants"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/kube"
)

type MeshDataplane interface {
	// called first, (even before Start()).
	ConstructInitialSnapshot(ambientPods []*corev1.Pod) error
	Start(ctx context.Context)

	//	IsPodInMesh(ctx context.Context, pod *metav1.ObjectMeta, netNs string) (bool, error)
	AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error
	RemovePodFromMesh(ctx context.Context, pod *corev1.Pod) error
	DelPodFromMesh(ctx context.Context, pod *corev1.Pod) error

	Stop()
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

	log.Debug("creating ipsets in the node netns")
	set, err := createHostsideProbeIpset()
	if err != nil {
		return nil, fmt.Errorf("error initializing hostside probe ipset: %w", err)
	}

	podNsMap := newPodNetnsCache(openNetnsInRoot(pconstants.HostMountsPath))
	ztunnelServer, err := newZtunnelServer(args.ServerSocket, podNsMap)
	if err != nil {
		return nil, fmt.Errorf("error initializing the ztunnel server: %w", err)
	}

	cfg := &iptables.Config{
		RestoreFormat: true,
		RedirectDNS:   args.DNSCapture,
	}

	iptablesConfigurator, err := iptables.NewIptablesConfigurator(cfg, realDependencies(), iptables.RealNlDeps())
	if err != nil {
		return nil, fmt.Errorf("error configuring iptables: %w", err)
	}

	// Create hostprobe rules now, in the host netns
	// Later we will reuse this same configurator inside the pod netns for adding other rules
	iptablesConfigurator.DeleteHostRules()

	if err := iptablesConfigurator.CreateHostRulesForHealthChecks(&HostProbeSNATIP); err != nil {
		return nil, fmt.Errorf("error initializing the host rules for health checks: %w", err)
	}

	podNetns := NewPodNetnsProcFinder(os.DirFS(filepath.Join(pconstants.HostMountsPath, "proc")))
	netServer := newNetServer(ztunnelServer, podNsMap, iptablesConfigurator, podNetns, set)

	// Set some defaults
	s := &Server{
		ctx:        ctx,
		kubeClient: client,
		isReady:    ready,
		dataplane: &meshDataplane{
			kubeClient: client.Kube(),
			netServer:  netServer,
		},
	}
	s.NotReady()
	s.handlers = setupHandlers(s.ctx, s.kubeClient, s.dataplane, args.SystemNamespace)

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

// createHostsideProbeIpset creates an ipset. This is designed to be called from the host netns.
// Note that if the ipset already exist by name, Create will not return an error.
//
// We will unconditionally flush our set before use here, so it shouldn't matter.
func createHostsideProbeIpset() (ipset.IPSet, error) {
	linDeps := ipset.RealNlDeps()
	probeSet, err := ipset.NewIPSet(constants.ProbeIPSet, linDeps)
	if err != nil {
		return probeSet, err
	}
	probeSet.Flush()
	return probeSet, nil
}

func (s *Server) Start() {
	log.Info("CNI ambient server starting")
	s.kubeClient.RunAndWait(s.ctx.Done())
	log.Info("CNI ambient server kubeclient started")
	pods := s.handlers.GetAmbientPods()
	err := s.dataplane.ConstructInitialSnapshot(pods)
	if err != nil {
		log.Warnf("failed to construct initial snapshot: %v", err)
	}

	log.Info("CNI ambient server marking ready")
	s.Ready()
	s.dataplane.Start(s.ctx)
	s.handlers.Start()
}

func (s *Server) Stop() {
	log.Info("CNI ambient server terminating, cleaning up node net rules")

	s.cniServerStopFunc()
	s.dataplane.Stop()
}

type meshDataplane struct {
	kubeClient kubernetes.Interface
	netServer  MeshDataplane
}

func (s *meshDataplane) Start(ctx context.Context) {
	s.netServer.Start(ctx)
}

func (s *meshDataplane) Stop() {
	s.netServer.Stop()
}

func (s *meshDataplane) ConstructInitialSnapshot(ambientPods []*corev1.Pod) error {
	return s.netServer.ConstructInitialSnapshot(ambientPods)
}

func (s *meshDataplane) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	var retErr error
	err := s.netServer.AddPodToMesh(ctx, pod, podIPs, netNs)
	if err != nil {
		log.Errorf("failed to add pod to ztunnel: %v", err)
		if !errors.Is(err, ErrPartialAdd) {
			return err
		}
		retErr = err
	}

	log.Debugf("annotating pod %s", pod.Name)
	if err := util.AnnotateEnrolledPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		log.Errorf("failed to annotate pod enrollment: %v", err)
		// don't return error here, as this is purely informational.
	}
	return retErr
}

func (s *meshDataplane) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	err := s.netServer.RemovePodFromMesh(ctx, pod)
	if err != nil {
		log.Errorf("failed to remove pod from mesh: %v", err)
		return err
	}
	log.Debug("removing annotation from pod")
	err = util.AnnotateUnenrollPod(s.kubeClient, &pod.ObjectMeta)
	if err != nil {
		log.Errorf("failed to annotate pod unenrollment: %v", err)
	}
	return err
}

// Delete pod from mesh: pod is deleted. iptables rules will die with it, we just need to update ztunnel
func (s *meshDataplane) DelPodFromMesh(ctx context.Context, pod *corev1.Pod) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	err := s.netServer.DelPodFromMesh(ctx, pod)
	if err != nil {
		log.Errorf("failed to delete pod from mesh: %v", err)
		return err
	}
	return nil
}
