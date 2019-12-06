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
	"fmt"
	"os"
	"path/filepath"

	"istio.io/istio/pkg/kube/inject"
	"istio.io/pkg/log"
)

func (s *Server) initSidecarInjector(args *PilotArgs) error {
	injectPath := args.InjectionOptions.InjectionDirectory
	if injectPath == "" {
		log.Infof("Skipping sidecar injector, injection path not specified")
		return nil
	}
	// If the injection path exists, we will set up injection
	if _, err := os.Stat(injectPath); !os.IsNotExist(err) {
		dir, err := pilotDNSCertDir()
		if err != nil {
			return err
		}
		parameters := inject.WebhookParameters{
			ConfigFile: filepath.Join(injectPath, "config"),
			ValuesFile: filepath.Join(injectPath, "values"),
			MeshFile:   args.Mesh.ConfigFile,
			CertFile:   filepath.Join(dir, "cert-chain.pem"),
			KeyFile:    filepath.Join(dir, "key.pem"),
			Port:       args.InjectionOptions.Port,
		}

		wh, err := inject.NewWebhook(parameters)
		if err != nil {
			return fmt.Errorf("failed to create injection webhook: %v", err)
		}
		s.addStartFunc(func(stop <-chan struct{}) error {
			go wh.Run(stop)
			return nil
		})
		return nil
	}
	log.Infof("Skipping sidecar injector, template not found")
	return nil
}
