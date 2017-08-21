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

package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/golang/glog"

	"istio.io/pilot/platform/kube/inject"
	"istio.io/pilot/test/util"
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
func (a *accessLogs) check(infra *infra) error {
	if !infra.checkLogs {
		glog.Info("Log checking is disabled")
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	glog.Info("Checking pod logs for request IDs...")
	glog.V(3).Info(a.logs)

	funcs := make(map[string]func() status)
	for app := range a.logs {
		name := fmt.Sprintf("Checking log of %s", app)
		funcs[name] = (func(app string) func() status {
			return func() status {
				if len(infra.apps[app]) == 0 {
					return fmt.Errorf("missing pods for app %q", app)
				}

				pod := infra.apps[app][0]
				container := inject.ProxyContainerName
				if app == "mixer" {
					container = "mixer"
				}
				logs := util.FetchLogs(client, pod, infra.Namespace, container)

				if strings.Contains(logs, "segmentation fault") {
					return fmt.Errorf("segmentation fault %s", pod)
				}

				if strings.Contains(logs, "assert failure") {
					return fmt.Errorf("assert failure in %s", pod)
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
						glog.Errorf("Got %d for %s in logs of %s, want %d", got, id, pod, want)
						return errAgain
					}
				}

				return nil
			}
		})(app)
	}
	return parallel(funcs)
}
