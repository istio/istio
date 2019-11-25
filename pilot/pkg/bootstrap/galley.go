// Copyright 2019 Istio Authors
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

package bootstrap

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/pkg/log"
)

var (
	// GalleyOverride is an optional json file, in the format defined by the galley.Args struct, to patch
	// galley settings. It can be mounted as a config map for users who need advanced features - until a proper
	// high-level API ( MeshConfig or Istio API ) is defined if the use case is common enough.
	// Break-glass only, version-specific.
	GalleyOverride = "./var/lib/istio/galley/galley.json"
)

func (s *Server) initGalley(args *PilotArgs) error {
	if s.kubeClient == nil {
		// The default galley config is based on k8s - will fail otherwise.
		// TODO: if file-based sources are configured, continue with the setup ( standalone )
		return nil
	}
	if useExternalGalley.Get() {
		// currently off by default - until dashboard tests are fixed.
		// It appears the buffers run out or some other issues happen.
		return nil
	}

	// Galley args
	gargs := settings.DefaultArgs()

	// Default dir.
	// If not set, will use K8S.
	//  gargs.ConfigPath = baseDir + "/var/lib/istio/local"
	// TODO: load a json file to override defaults (for all components)

	gargs.EnableServer = true
	gargs.InsecureGRPC = s.grpcServer
	gargs.SecureGRPC = s.secureGRPCServerDNS

	if _, err := os.Stat(DNSCertDir + "/key.pem"); err == nil {
		gargs.ValidationArgs.EnableValidation = true
		gargs.ValidationArgs.CACertFile = DNSCertDir + "/root-cert.pem"
		gargs.ValidationArgs.CertFile = DNSCertDir + "/cert-chain.pem"
		gargs.ValidationArgs.KeyFile = DNSCertDir + "/key.pem"
		// Picking a different port from injector - which is on basePort + 443
		// TODO: share the same port for all 3.
		gargs.ValidationArgs.Port = uint(s.basePort + 444)

		gargs.ValidationArgs.Mux = s.mux
	} else {
		gargs.ValidationArgs.EnableValidation = false
	}

	// TODO: use the same code as Injection - same port, cert, etc.
	gargs.ValidationArgs.EnableReconcileWebhookConfiguration = false

	gargs.Readiness.Path = "/tmp/healthReadiness"
	gargs.APIAddress = fmt.Sprintf("tcp://0.0.0.0:%d", s.basePort+901)

	// TODO: For secure, we'll expose the GRPC register method and use the common GRPC+TLS port.
	gargs.Insecure = true
	gargs.DisableResourceReadyCheck = true

	// Use Galley Ctrlz for all services.
	gargs.IntrospectionOptions.Port = uint16(s.basePort + 876)

	gargs.KubeRestConfig = s.kubeRestConfig
	gargs.KubeInterface = s.kubeClientset

	// TODO: add to mesh.yaml - possibly using same model as tracers/etc

	if _, err := os.Stat(GalleyOverride); err == nil {
		overrideGalley, err := ioutil.ReadFile(GalleyOverride)
		if err != nil {
			log.Fatalf("Failed to read overrides %v", err)
		}
		err = json.Unmarshal(overrideGalley, gargs)
		if err != nil {
			log.Fatalf("Failed to parse overrides %v", err)
		}
	}

	meshCfgFile := args.Mesh.ConfigFile

	// The file is loaded and watched by Galley using galley/pkg/meshconfig watcher/reader
	// Current code in galley doesn't expose it - we'll use 2 Caches instead.

	// Defaults are from pkg/config/mesh

	// Actual files are loaded by galley/pkg/src/fs, which recursively loads .yaml and .yml files
	// The files are suing YAMLToJSON, but interpret Kind, APIVersion

	// This is the 'mesh' file served by Galley - not clear who is using it, ideally we should drop it.
	// It is based on default configs, will include overrides from user, merged CRD, etc.
	// TODO: when the mesh.yaml is reloaded, replace the file watched by Galley as well.
	if _, err := os.Stat(meshCfgFile); err != nil {
		// Galley requires this file to exist. Create it in a writeable directory, override.
		meshBytes, err := json.Marshal(s.mesh)
		if err != nil {
			return fmt.Errorf("failed to serialize mesh %v", err)
		}
		err = ioutil.WriteFile("/tmp/mesh", meshBytes, 0700)
		if err != nil {
			return fmt.Errorf("failed to serialize mesh %v", err)
		}
		meshCfgFile = "/tmp/mesh"
	}

	gargs.MeshConfigFile = meshCfgFile
	gargs.MonitoringPort = uint(s.basePort + 15)

	// Galley component
	// TODO: runs under same gRPC port.
	s.Galley = server.New(gargs)

	s.addStartFunc(func(stop <-chan struct{}) error {
		if err := s.Galley.Start(); err != nil {
			log.Fatalf("Error creating server: %v", err)
		}
		return nil
	})

	return nil
}

// WIP: direct integration of the Galley source to Pilot model.
type pilotModelHandler struct {
}

func (t pilotModelHandler) Handle(e event.Event) {
	log.Debugf("Event %v", e)
}

func (s *Server) NewGalleyK8SSource(resources schema.KubeResources) (src event.Source, err error) {

	o := apiserver.Options{
		Client:       kube.NewInterfaces(s.kubeRestConfig),
		ResyncPeriod: s.Args.Config.ControllerOptions.ResyncPeriod,
		Resources:    resources,
	}
	src = apiserver.New(o)

	src.Dispatch(pilotModelHandler{})

	return
}
