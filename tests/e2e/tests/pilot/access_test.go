// Copyright 2017 Istio Authors
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

package pilot

import (
	"fmt"
	"strings"
	"sync"

	"istio.io/istio/pilot/pkg/kube/inject"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"

	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

// envoy access log testing utilities

// accessLogs collects test expectations for access logs
type accessLogs struct {
	mu sync.Mutex

	// logs is a mapping from app name to requests
	logs map[string][]request
}

type request struct {
	id   string
	desc string
}

func makeAccessLogs() *accessLogs {
	return &accessLogs{
		logs: make(map[string][]request),
	}
}

// add an access log entry for an app
func (a *accessLogs) add(app, id, desc string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs[app] = append(a.logs[app], request{id: id, desc: desc})
}

// check logs against a deployment
func (a *accessLogs) check(e *tutil.Environment) error {
	if !e.Config.CheckLogs {
		log.Info("Log checking is disabled")
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	log.Info("Checking pod logs for request IDs...")
	log.Debuga(a.logs)

	funcs := make(map[string]func() tutil.Status)
	for app := range a.logs {
		name := fmt.Sprintf("Checking log of %s", app)
		funcs[name] = (func(app string) func() tutil.Status {
			return func() tutil.Status {
				if len(e.Apps[app]) == 0 {
					return fmt.Errorf("missing pods for app %q", app)
				}

				pod := e.Apps[app][0]
				container := inject.ProxyContainerName
				ns := e.Config.Namespace
				switch app {
				case "mixer":
					container = "mixer"
					ns = e.Config.IstioNamespace
				case "ingress":
					ns = e.Config.IstioNamespace
				}
				util.CopyPodFiles(container, pod, ns, model.ConfigPathDir, e.Config.CoreFilesDir+"/"+pod+"."+ns)
				logs := util.FetchLogs(e.KubeClient, pod, ns, container)

				if strings.Contains(logs, "segmentation fault") {
					util.CopyPodFiles(container, pod, ns, model.ConfigPathDir, e.Config.CoreFilesDir+"/"+pod+"."+ns)
					return fmt.Errorf("segmentation fault %s log: %s", pod, logs)
				}

				if strings.Contains(logs, "assert failure") {
					util.CopyPodFiles(container, pod, ns, model.ConfigPathDir, e.Config.CoreFilesDir+"/"+pod+"."+ns)
					return fmt.Errorf("assert failure in %s log: %s", pod, logs)
				}

				// find all ids and counts
				// TODO: this can be optimized for many string submatching
				counts := make(map[string]int)
				for _, request := range a.logs[app] {
					counts[request.id] = counts[request.id] + 1
				}
				for id, want := range counts {
					got := strings.Count(logs, id)
					if got < want {
						log.Errorf("Got %d for %s in logs of %s, want %d", got, id, pod, want)
						log.Errorf("Log: %s", logs)
						return tutil.ErrAgain
					}
				}

				return nil
			}
		})(app)
	}
	return tutil.Parallel(funcs)
}
