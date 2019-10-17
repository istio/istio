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

package istiod

import (
	"log"
	"os"
	"time"

	"istio.io/istio/pilot/pkg/features"

	meshv1 "istio.io/api/mesh/v1alpha1"
	agent "istio.io/istio/pkg/bootstrap"
)

// Envoy sidecar starting. The combined binary will start a sidecar if certs are present.
// To simplify, the envoy is running alongside control plane binary, same container.
// This can also be used in applications that run a sidecar directly.

// Should be called at the end, if we receive SIGINT or SIGTERM
func DrainEnvoy(base string, cfg *meshv1.ProxyConfig) {
	// Simplified version:
	// - hot restart envoy with new config
	// - sleep terminationDrainDuration
	// - exit

	stop := make(chan error)
	//features.EnvoyBaseId.DefaultValue = "1"
	process, err := agent.RunProxy(cfg, "nodeid", 2,
		base+"/etc/istio/proxy/envoy_bootstrap_drain.json", stop,
		os.Stdout, os.Stderr, []string{
			// "--disable-hot-restart",
			// "-l", "trace",
		})

	if err != nil {
		log.Fatal("Failed to drain, abrupt termination", err)
	}

	go func() {
		// Should not happen.
		process.Wait()
		log.Fatal("Envoy terminated after drain")
	}()

	// Env variable from Istio
	time.Sleep(features.TerminationDrainDuration())
}
