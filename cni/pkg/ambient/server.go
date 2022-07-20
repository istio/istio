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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	listerv1 "k8s.io/client-go/listers/core/v1"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/cni/pkg/ambient/constants"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/server"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/mesh"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
)

type Server struct {
	kubeClient       kube.Client
	environment      *model.Environment
	fileWathcer      filewatcher.FileWatcher
	shutdownDuration time.Duration
	internalStop     chan struct{}
	server           server.Instance
	queue            controllers.Queue

	nsLister listerv1.NamespaceLister

	meshMode          v1alpha1.MeshConfig_AmbientMeshConfig_AmbientMeshMode
	disabledSelectors []*metav1.LabelSelector
	mu                sync.Mutex
	uproxyRunning     bool
}

type AmbientConfigFile struct {
	Mode              string                  `json:"mode"`
	DisabledSelectors []*metav1.LabelSelector `json:"disabledSelectors"`
	UproxyReady       bool                    `json:"uproxyReady"`
}

var (
	ErrRouteList   = errors.New("getting route list")
	ErrRouteDelete = errors.New("deleting route")
)

func NewServer(args *AmbientArgs) (*Server, error) {
	e := &model.Environment{
		PushContext: model.NewPushContext(),
	}
	// Set some defaults
	s := &Server{
		fileWathcer:       filewatcher.NewWatcher(),
		shutdownDuration:  args.ShutdownDuration,
		environment:       e,
		internalStop:      make(chan struct{}),
		server:            server.New(),
		meshMode:          v1alpha1.MeshConfig_AmbientMeshConfig_DEFAULT,
		disabledSelectors: legacySelectors,
		uproxyRunning:     false,
	}

	if err := s.initKubeClient(args); err != nil {
		return nil, fmt.Errorf("error initializing kube client: %v", err)
	}

	args.RegistryOptions.KubeOptions.EndpointMode = kubecontroller.DetectEndpointMode(s.kubeClient)

	// We need to find our Host IP -- is there a better way to do this?
	h, err := GetHostIP(s.kubeClient.Kube())
	if err != nil || h == "" {
		return nil, fmt.Errorf("error getting host IP: %v", err)
	}
	HostIP = h
	log.Infof("HostIP=%v", HostIP)

	s.initMeshConfiguration(args, s.fileWathcer)
	s.environment.AddMeshHandler(s.newConfigMapWatcher)
	s.setupHandlers()

	if s.environment.Mesh().AmbientMesh != nil {
		s.mu.Lock()
		s.meshMode = s.environment.Mesh().AmbientMesh.Mode
		s.disabledSelectors = s.environment.Mesh().AmbientMesh.DisabledSelectors
		s.mu.Unlock()
	}

	s.UpdateConfig()

	// This must be last, otherwise we will not know which informers to register
	if s.kubeClient != nil {
		s.addStartFunc(func(stop <-chan struct{}) error {
			go func() {
				s.Run(stop)
			}()
			return nil
		})
		s.addStartFunc(func(stop <-chan struct{}) error {
			s.kubeClient.RunAndWait(stop)
			return nil
		})
	}

	return s, nil
}

func (s *Server) setUproxyRunning(running bool) {
	s.mu.Lock()
	s.uproxyRunning = running
	s.mu.Unlock()
	s.UpdateConfig()
}

func (s *Server) isUproxyRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uproxyRunning
}

// initKubeClient creates the k8s client if running in an k8s environment.
// This is determined by the presence of a kube registry, which
// uses in-context k8s, or a config source of type k8s.
func (s *Server) initKubeClient(args *AmbientArgs) error {
	if s.kubeClient != nil {
		// Already initialized by startup arguments
		return nil
	}
	hasK8SConfigStore := false
	if args.RegistryOptions.FileDir == "" {
		// If file dir is set - config controller will just use file.
		if _, err := os.Stat(args.MeshConfigFile); !os.IsNotExist(err) {
			meshConfig, err := mesh.ReadMeshConfig(args.MeshConfigFile)
			if err != nil {
				return fmt.Errorf("failed reading mesh config: %v", err)
			}
			if len(meshConfig.ConfigSources) == 0 && args.RegistryOptions.KubeConfig != "" {
				hasK8SConfigStore = true
			}
			for _, cs := range meshConfig.ConfigSources {
				if cs.Address == string(Kubernetes)+"://" {
					hasK8SConfigStore = true
					break
				}
			}
		} else if args.RegistryOptions.KubeConfig != "" {
			hasK8SConfigStore = true
		}
	}

	if hasK8SConfigStore || hasKubeRegistry(args.RegistryOptions.Registries) {
		// Used by validation
		kubeRestConfig, err := kube.DefaultRestConfig(args.RegistryOptions.KubeConfig, "", func(config *rest.Config) {
			config.QPS = args.RegistryOptions.KubeOptions.KubernetesAPIQPS
			config.Burst = args.RegistryOptions.KubeOptions.KubernetesAPIBurst
		})
		if err != nil {
			return fmt.Errorf("failed creating kube config: %v", err)
		}

		s.kubeClient, err = kube.NewClient(kube.NewClientConfigForRestConfig(kubeRestConfig))
		if err != nil {
			return fmt.Errorf("failed creating kube client: %v", err)
		}
	}

	return nil
}

// addStartFunc appends a function to be run. These are run synchronously in order,
// so the function should start a go routine if it needs to do anything blocking
func (s *Server) addStartFunc(fn server.Component) {
	s.server.RunComponent(fn)
}

func (s *Server) Start(stop <-chan struct{}) error {
	log.Infof("Starting ambient-ds")

	if err := s.server.Start(stop); err != nil {
		return err
	}

	s.waitForShutdown(stop)

	return nil
}

func (s *Server) waitForShutdown(stop <-chan struct{}) {
	go func() {
		<-stop
		close(s.internalStop)
		_ = s.fileWathcer.Close()
		s.cleanup()
	}()
}

// WaitUntilCompletion waits for everything marked as a "required termination" to complete.
// This should be called before exiting.
func (s *Server) WaitUntilCompletion() {
	s.server.Wait()
}

func (s *Server) UpdateConfig() {
	log.Debug("Generating new ambient config file")

	cfg := &AmbientConfigFile{
		Mode:              s.meshMode.String(),
		DisabledSelectors: s.disabledSelectors,
		UproxyReady:       s.isUproxyRunning(),
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
			Mode:        "OFF",
			UproxyReady: false,
		}, nil
	}

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	cfg := &AmbientConfigFile{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
