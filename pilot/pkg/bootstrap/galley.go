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
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/pkg/log"
	"os"
)

var (
	// GalleyOverride is an optional json file, in the format defined by the galley.Args struct, to patch
	// galley settings. It can be mounted as a config map for users who need advanced features - until a proper
	// high-level API ( MeshConfig or Istio API ) is defined if the use case is common enough.
	// Break-glass only, version-specific.
	GalleyOverride = "./var/lib/istio/galley/galley.json"
)

func (server *Server) initGalley(args *PilotArgs) error {
	// Galley args
	gargs := settings.DefaultArgs()

	// Default dir.
	// If not set, will use K8S.
	//  gargs.ConfigPath = baseDir + "/var/lib/istio/local"
	// TODO: load a json file to override defaults (for all components)

	gargs.EnableServer = true

	gargs.ValidationArgs.EnableValidation = true
	gargs.ValidationArgs.CACertFile = DNSCertDir + "/root-cert.pem"
	gargs.ValidationArgs.CertFile = DNSCertDir + "/cert-chain.pem"
	gargs.ValidationArgs.KeyFile = DNSCertDir + "/key.pem"

	gargs.Readiness.Path = "/tmp/healthReadiness"

	gargs.ValidationArgs.EnableReconcileWebhookConfiguration = false
	gargs.APIAddress = fmt.Sprintf("tcp://0.0.0.0:%d", server.basePort+901)
	// TODO: For secure, we'll expose the GRPC register method and use the common GRPC+TLS port.
	gargs.Insecure = true
	gargs.DisableResourceReadyCheck = true
	// Use Galley Ctrlz for all services.
	gargs.IntrospectionOptions.Port = uint16(server.basePort + 876)

	gargs.KubeRestConfig = server.KubeRestConfig
	gargs.KubeInterface = server.KubeClient

	// TODO: add to mesh.yaml - possibly using same model as tracers/etc

	if _, err := os.Stat(GalleyOverride); err == nil {
		overrideGalley, err := ioutil.ReadFile(GalleyOverride)
		if err != nil {
			log.Fatalf("Failed to read overrides %v", err)
		}
		json.Unmarshal(overrideGalley, gargs)
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
		meshBytes, err := json.Marshal(server.Mesh)
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
	gargs.MonitoringPort = uint(server.basePort + 15)
	// Galley component
	// TODO: runs under same gRPC port.
	server.Galley = NewGalleyServer(gargs)


	return nil
}

// GalleyServer component is the main config processing component that will listen to a config source and publish
// resources through an MCP server.

// This is a simplified startup for galley, specific for hyperistio/combined:
// - callout removed - standalone galley supports it, and should be used
// - acl removed - envoy and Istio RBAC should handle it
// - listener removed - common grpc server for all components, using Pilot's listener

// NewGalleyServer is the equivalent of the Galley CLI. No attempt  to optimize or reuse -
// for Pilot we plan to use a 'direct path', bypassing the gRPC layer. This provides max compat
// and less risks with existing galley.
func NewGalleyServer(a *settings.Args) *server.Server {
	s := server.New(a)

	return s
}

// Start implements process.Component
func (s *Server) StartGalley() (err error) {
	if err := s.Galley.Start(); err != nil {
		log.Fatalf("Error creating server: %v", err)
	}
	return nil
}

type testHandler struct {
}

func (t testHandler) Handle(e event.Event) {
	log.Debugf("Event %v", e)
}

func (s *Server) NewGalleyK8SSource(resources schema.KubeResources) (src event.Source, err error) {

	o := apiserver.Options{
		Client:       kube.NewInterfaces(s.KubeRestConfig),
		ResyncPeriod: s.Args.Config.ControllerOptions.ResyncPeriod,
		Resources:    resources,
	}
	src = apiserver.New(o)

	src.Dispatch(testHandler{})

	return
}
