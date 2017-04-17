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

// Reachability tests

package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/test/util"
)

type reachability struct {
	*infra

	accessMu sync.Mutex

	// accessLogs is a mapping from app name to a list of request ids that should be present in it
	accessLogs map[string][]string

	// mixerLogs is a collection of request IDs that we expect to find in the mixer logs
	mixerLogs map[string]string
}

func (r *reachability) String() string {
	return "HTTP reachability"
}

func (r *reachability) setup() error {
	r.mixerLogs = make(map[string]string)
	r.accessLogs = make(map[string][]string)
	for app := range r.apps {
		r.accessLogs[app] = make([]string, 0)
	}
	return nil
}

func (r *reachability) teardown() {
}

func (r *reachability) run() error {
	glog.Info("Verifying basic reachability across pods/services (a, b, and t)..")
	if err := r.makeRequests(); err != nil {
		return err
	}
	if err := r.checkProxyAccessLogs(r.accessLogs); err != nil {
		return err
	}
	if r.checkLogs {
		if err := r.checkMixerLogs(); err != nil {
			return err
		}
	}
	return nil
}

// makeRequests executes requests in pods and collects request ids per pod to check against access logs
func (r *reachability) makeRequests() error {
	testPods := []string{"a", "b"}
	if r.Auth == proxyconfig.ProxyMeshConfig_NONE {
		// t is not behind proxy, so it cannot talk in Istio auth.
		testPods = append(testPods, "t")
	}
	funcs := make(map[string]func() status)
	for _, src := range testPods {
		for _, dst := range testPods {
			for _, port := range []string{"", ":80", ":8080"} {
				for _, domain := range []string{"", "." + r.Namespace} {
					name := fmt.Sprintf("HTTP request from %s to %s%s%s", src, dst, domain, port)
					funcs[name] = (func(src, dst, port, domain string) func() status {
						url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
						return func() status {
							request, err := util.Shell(fmt.Sprintf("kubectl exec %s -n %s -c app -- client -url %s",
								r.apps[src][0], r.Namespace, url))
							if err != nil {
								glog.Error(err)
								return failure
							}
							match := regexp.MustCompile("X-Request-Id=(.*)").FindStringSubmatch(request)
							if len(match) > 1 {
								id := match[1]
								r.accessMu.Lock()
								if src != "t" {
									r.accessLogs[src] = append(r.accessLogs[src], id)
								}
								if dst != "t" {
									r.accessLogs[dst] = append(r.accessLogs[dst], id)
								}

								// TODO no logs when source and destination is same (e.g. from "a" pod to "a" pod)
								// server side should have a proxy, so skip "t" destined requests
								if src != dst && dst != "t" {
									r.mixerLogs[id] = fmt.Sprintf("from %s to %s, port %s", src, dst, port)
								}
								r.accessMu.Unlock()

								return success
							}
							if src == "t" && dst == "t" {
								// Expected no match for t->t
								return success
							}
							return again
						}
					})(src, dst, port, domain)
				}
			}
		}
	}
	return parallel(funcs)
}

func (r *reachability) checkMixerLogs() error {
	glog.Info("Checking mixer logs for request IDs...")

	for n := 0; n < budget; n++ {
		found := true
		access := util.FetchLogs(client, r.apps["mixer"][0], r.Namespace, "mixer")

		for id, desc := range r.mixerLogs {
			if !strings.Contains(access, id) {
				glog.Infof("Failed to find request id %s for %s in mixer logs\n", id, desc)
				found = false
				break
			}
		}

		if found {
			return nil
		}

		time.Sleep(time.Second)
	}
	return fmt.Errorf("exceeded budget for checking mixer logs")
}
