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

// nolint: revive
package fuzz

import (
	"os"
	"time"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pkg/config/mesh"
)

func FuzzNewBootstrapServer(data []byte) int {
	f := fuzz.NewConsumer(data)

	// Create mesh config file
	meshConfigFile, err := createRandomConfigFile(f)
	if err != nil {
		return 0
	}
	defer os.Remove(meshConfigFile)
	_, err = os.Stat(meshConfigFile)
	if err != nil {
		return 0
	}
	// Validate mesh config file
	meshConfigYaml, err := mesh.ReadMeshConfigData(meshConfigFile)
	if err != nil {
		return 0
	}
	_, err = mesh.ApplyMeshConfigDefaults(meshConfigYaml)
	if err != nil {
		return 0
	}

	// Create kube config file
	kubeConfigFile, err := createRandomConfigFile(f)
	if err != nil {
		return 0
	}
	defer os.Remove(kubeConfigFile)

	args := &bootstrap.PilotArgs{}
	err = f.GenerateStruct(args)
	if err != nil {
		return 0
	}

	args.MeshConfigFile = meshConfigFile
	args.RegistryOptions.KubeConfig = kubeConfigFile
	args.ShutdownDuration = 1 * time.Millisecond

	stop := make(chan struct{})
	s, err := bootstrap.NewServer(args)
	if err != nil {
		return 0
	}
	err = s.Start(stop)
	if err != nil {
		return 0
	}
	defer func() {
		close(stop)
		s.WaitUntilCompletion()
	}()
	return 1
}
