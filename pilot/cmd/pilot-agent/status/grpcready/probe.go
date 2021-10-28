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

package grpcready

import (
	"fmt"
	"sync"

	"istio.io/istio/pilot/cmd/pilot-agent/status/ready"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/istio-agent/grpcxds"
)

var _ ready.Prober = &probe{}

type probe struct {
	sync.RWMutex
	bootstrapPath string
	bootstrap     *grpcxds.Bootstrap
}

// NewProbe returns a probe that checks if a valid bootstrap file can be loaded once.
// If that bootstrap file has a file_watcher cert provider, we also ensure those certs exist.
func NewProbe(bootstrapPath string) ready.Prober {
	return &probe{bootstrapPath: bootstrapPath}
}

func (p *probe) Check() error {
	// TODO file watch?
	if p.getBootstrap() == nil {
		bootstrap, err := grpcxds.LoadBootstrap(p.bootstrapPath)
		if err != nil {
			return fmt.Errorf("failed loading %s: %v", p.bootstrapPath, err)
		}
		p.setBootstrap(bootstrap)
	}
	if bootstrap := p.getBootstrap(); bootstrap != nil {
		if fwp := bootstrap.FileWatcherProvider(); fwp != nil {
			for _, path := range fwp.FilePaths() {
				if !file.Exists(path) {
					return fmt.Errorf("%s does not exist", path)
				}
			}
		}
	}
	return nil
}

func (p *probe) getBootstrap() *grpcxds.Bootstrap {
	p.RLock()
	defer p.RUnlock()
	return p.bootstrap
}

func (p *probe) setBootstrap(bootstrap *grpcxds.Bootstrap) {
	p.Lock()
	defer p.Unlock()
	p.bootstrap = bootstrap
}
