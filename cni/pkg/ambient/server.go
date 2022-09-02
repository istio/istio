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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/cni/pkg/ambient/constants"
	"istio.io/istio/pilot/pkg/ambient/ambientpod"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
)

type Server struct {
	kubeClient  kube.Client
	environment *model.Environment
	ctx         context.Context
	queue       controllers.Queue

	nsLister listerv1.NamespaceLister

	meshMode          v1alpha1.MeshConfig_AmbientMeshConfig_AmbientMeshMode
	disabledSelectors []*metav1.LabelSelector
	mu                sync.Mutex
	ztunnelRunning    bool
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
		environment:       e,
		ctx:               ctx,
		meshMode:          v1alpha1.MeshConfig_AmbientMeshConfig_DEFAULT,
		disabledSelectors: ambientpod.LegacySelectors,
		ztunnelRunning:    false,
		kubeClient:        client,
	}

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
		s.disabledSelectors = s.environment.Mesh().AmbientMesh.DisabledSelectors
		s.mu.Unlock()
	}

	s.UpdateConfig()

	return s, nil
}

func (s *Server) setZTunnelRunning(running bool) {
	s.mu.Lock()
	s.ztunnelRunning = running
	s.mu.Unlock()
	s.UpdateConfig()
}

func (s *Server) isZTunnelRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ztunnelRunning
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
		DisabledSelectors: s.disabledSelectors,
		ZTunnelReady:      s.isZTunnelRunning(),
	}

	if err := cfg.write(); err != nil {
		log.Errorf("Failed to write config file: %v", err)
	}
	log.Debug("Done")
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
