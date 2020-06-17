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

package perf

import (
	"fmt"
	"os"
	"path"
	"time"
)

// ServiceLocation is a struct that combines the address and the path of an rpc server.
type ServiceLocation struct {
	// Address is the network address of the service.
	Address string

	// Path is the HTTP path of the RPC service.
	Path string
}

func (s ServiceLocation) String() string {
	return s.Address + s.Path
}

// generatePath is used to generate the HTTP rpc path for a given component.
func generatePath(component string) string {
	discriminator := fmt.Sprintf("ns%d", time.Now().Nanosecond())
	return fmt.Sprintf("/%s/perf/mixer/%s/_goRPC_", component, discriminator)
}

// generateDebugPath is used to generate the HTTP rpc debug path for a given component.
func generateDebugPath(component string) string {
	discriminator := fmt.Sprintf("nsd%d", time.Now().Nanosecond())
	return fmt.Sprintf("/debug/%s/perf/mixer/%s/rpc", component, discriminator)
}

// locatePerfClientProcess will walk the directory tree and try to find the process that should be executed
// to run perf tests in co-process mode.
func locatePerfClientProcess(clientExecRelativePath string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for dir := wd; dir != "."; dir = path.Dir(dir) {
		candidate := path.Join(dir, clientExecRelativePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("perfclient not found in '%s'", wd)
}
