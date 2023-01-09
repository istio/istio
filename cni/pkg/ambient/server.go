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

package ambient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/cni/pkg/ambient/constants"
	"istio.io/istio/pilot/pkg/ambient/ambientpod"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/lazy"
)

type Server struct {
	kubeClient  kube.Client
	environment *model.Environment
	ctx         context.Context
	queue       controllers.Queue

	nsLister  listerv1.NamespaceLister
	podLister listerv1.PodLister

	meshMode          v1alpha1.MeshConfig_AmbientMeshConfig_AmbientMeshMode
	disabledSelectors []labels.Selector
	// disabledSelectors can be used to filter objects, but not to marshal. So we have 2 copies:
	// one that can be marshaled, and one that can select.
	marshalableDisabledSelectors []*metav1.LabelSelector
	mu                           sync.Mutex
	ztunnelPod                   *corev1.Pod

	iptablesCommand lazy.Lazy[string]
}

type AmbientConfigFile struct {
	Mode              string                  `json:"mode"`
	DisabledSelectors []*metav1.LabelSelector `json:"disabledSelectors"`
	ZTunnelReady      bool                    `json:"ztunnelReady"`
}

func NewServer(ctx context.Context, args AmbientArgs) (*Server, error) {
	e := &model.Environment{
		PushContext: model.NewPushContext(),
	}
	client, err := buildKubeClient(args.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}
	// Set some defaults
	s := &Server{
		environment:                  e,
		ctx:                          ctx,
		meshMode:                     v1alpha1.MeshConfig_AmbientMeshConfig_DEFAULT,
		disabledSelectors:            ambientpod.LegacySelectors,
		marshalableDisabledSelectors: ambientpod.LegacyLabelSelector,
		kubeClient:                   client,
	}
	s.iptablesCommand = lazy.New(func() (string, error) {
		return s.detectIptablesCommand(), nil
	})

	// We need to find our Host IP -- is there a better way to do this?
	h, err := GetHostIP(s.kubeClient.Kube())
	if err != nil || h == "" {
		return nil, fmt.Errorf("error getting host IP: %v", err)
	}
	HostIP = h
	log.Infof("HostIP=%v", HostIP)

	s.initMeshConfiguration(args)
	s.environment.AddMeshHandler(s.newConfigMapWatcher)
	s.setupHandlers()

	if s.environment.Mesh().AmbientMesh != nil {
		s.mu.Lock()
		s.meshMode = s.environment.Mesh().AmbientMesh.Mode
		s.disabledSelectors = ambientpod.ConvertDisabledSelectors(s.environment.Mesh().AmbientMesh.DisabledSelectors)
		s.marshalableDisabledSelectors = s.environment.Mesh().AmbientMesh.DisabledSelectors
		s.mu.Unlock()
	}

	s.UpdateConfig()

	return s, nil
}

func (s *Server) isZTunnelRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ztunnelPod != nil
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

	client, err := kube.NewClient(kube.NewClientConfigForRestConfig(kubeRestConfig))
	if err != nil {
		return nil, fmt.Errorf("failed creating kube client: %v", err)
	}

	return client, nil
}

func (s *Server) Start() {
	s.kubeClient.RunAndWait(s.ctx.Done())
	go func() {
		s.queue.Run(s.ctx.Done())
		s.cleanup()
	}()
}

func (s *Server) UpdateConfig() {
	log.Debug("Generating new ambient config file")

	cfg := &AmbientConfigFile{
		Mode:              s.meshMode.String(),
		DisabledSelectors: s.marshalableDisabledSelectors,
		ZTunnelReady:      s.isZTunnelRunning(),
	}

	if err := cfg.write(); err != nil {
		log.Errorf("Failed to write config file: %v", err)
	}
	log.Debug("Done")
}

var ztunnelLabels = labels.SelectorFromValidatedSet(labels.Set{"app": "ztunnel"})

func (s *Server) ReconcileZtunnel() error {
	pods, err := s.podLister.Pods(metav1.NamespaceAll).List(ztunnelLabels)
	if err != nil {
		return err
	}
	var activePod *corev1.Pod
	for _, p := range pods {
		ready := kube.CheckPodReady(p) == nil
		if !ready {
			continue
		}
		if activePod == nil {
			// Only pod ready, mark this as active
			activePod = p
		} else if p.CreationTimestamp.After(activePod.CreationTimestamp.Time) {
			// If we have multiple pods that are ready, use the newest one.
			// This ensures on a rolling update we start sending traffic to the new pod and drain the old one.
			activePod = p
		}
	}

	needsUpdate := false
	s.mu.Lock()
	if getUID(s.ztunnelPod) != getUID(activePod) {
		// Active pod change
		s.ztunnelPod = activePod
		needsUpdate = true
	}
	s.mu.Unlock()

	if !needsUpdate {
		log.Debugf("active ztunnel unchanged")
		return nil
	}
	s.UpdateConfig()
	if activePod == nil {
		log.Infof("active ztunnel updated, no ztunnel running on the node")
		s.cleanup()
		return nil
	}
	log.Infof("active ztunnel updated to %v", activePod.Name)
	// TODO: we should not cleanup and recreate; this has downtime. We should mutate the existing rules in place
	s.cleanup()
	veth, err := getDeviceWithDestinationOf(activePod.Status.PodIP)
	if err != nil {
		return fmt.Errorf("failed to get device: %v", err)
	}

	captureDNS := getEnvFromPod(activePod, "ISTIO_META_DNS_CAPTURE") == "true"
	err = s.CreateRulesOnNode(veth, activePod.Status.PodIP, captureDNS)
	if err != nil {
		return fmt.Errorf("failed to configure node for ztunnel: %v", err)
	}
	// Reconcile namespaces, as it is possible for the original reconciliation to have failed, and a
	// small pod to have started up before ztunnel is running... so we need to go back and make sure we
	// catch the existing pods
	s.ReconcileNamespaces()
	return nil
}

// getUID is a nil safe UID accessor
func getUID(o *corev1.Pod) types.UID {
	if o == nil {
		return ""
	}
	return o.GetUID()
}

func (c *AmbientConfigFile) write() error {
	configFile := constants.AmbientConfigFilepath

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	log.Infof("Writing ambient config: %s", data)

	return atomicWrite(configFile, data)
}

func atomicWrite(filename string, data []byte) error {
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpFile, filename)
}

func ReadAmbientConfig() (*AmbientConfigFile, error) {
	configFile := constants.AmbientConfigFilepath

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return &AmbientConfigFile{
			Mode:         "OFF",
			ZTunnelReady: false,
		}, nil
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	cfg := &AmbientConfigFile{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
