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
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/golang/sync/errgroup"
)

type reachability struct {
	accessMu sync.Mutex

	// accessLogs is a mapping from app name to a list of request ids that should be present in it
	accessLogs map[string][]string
}

func (r *reachability) run() error {
	glog.Info("Verifying basic reachability across pods/services (a, b, and t)..")

	r.accessLogs = make(map[string][]string)
	for app := range pods {
		r.accessLogs[app] = make([]string, 0)
	}

	if err := r.makeRequests(); err != nil {
		return err
	}

	if err := r.verifyTCPRouting(); err != nil {
		return err
	}

	if glog.V(2) {
		glog.Infof("requests: %#v", r.accessLogs)
	}

	if params.logs {
		if err := r.checkProxyAccessLogs(); err != nil {
			return err
		}
		if err := r.checkMixerLogs(); err != nil {
			return err
		}
	}

	glog.Info("Success!")
	return nil
}

// makeRequest creates a function to make requests; done should return true to quickly exit the retry loop
func (r *reachability) makeRequest(src, dst, port, domain string, done func() bool) func() error {
	return func() error {
		url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
		for n := 0; n < budget; n++ {
			glog.Infof("Making a request %s from %s (attempt %d)...\n", url, src, n)
			request, err := shell(fmt.Sprintf("kubectl exec %s -n %s -c app -- client -url %s",
				pods[src], params.namespace, url))
			if err != nil {
				return err
			}
			match := regexp.MustCompile("X-Request-Id=(.*)").FindStringSubmatch(request)
			if len(match) > 1 {
				id := match[1]
				glog.V(2).Infof("id=%s\n", id)
				r.accessMu.Lock()
				r.accessLogs[src] = append(r.accessLogs[src], id)
				r.accessLogs[dst] = append(r.accessLogs[dst], id)
				r.accessMu.Unlock()
				return nil
			}

			// Expected no match
			if src == "t" && dst == "t" {
				glog.V(2).Info("Expected no match for t->t")
				return nil
			}
			if done() {
				return nil
			}
		}
		return fmt.Errorf("failed to inject proxy from %s to %s (url %s)", src, dst, url)
	}
}

// makeRequests executes requests in pods and collects request ids per pod to check against access logs
func (r *reachability) makeRequests() error {
	g, ctx := errgroup.WithContext(context.Background())
	testPods := []string{"a", "b", "t"}
	for _, src := range testPods {
		for _, dst := range testPods {
			for _, port := range []string{"", ":80", ":8080"} {
				for _, domain := range []string{"", "." + params.namespace} {
					if params.parallel {
						g.Go(r.makeRequest(src, dst, port, domain, func() bool {
							select {
							case <-time.After(time.Second):
								// try again
							case <-ctx.Done():
								return true
							}
							return false
						}))
					} else {
						if err := r.makeRequest(src, dst, port, domain, func() bool { return false })(); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	if params.parallel {
		if err := g.Wait(); err != nil {
			return err
		}
	}
	return nil
}

func (r *reachability) checkProxyAccessLogs() error {
	glog.Info("Checking access logs of pods to correlate request IDs...")
	for n := 0; n < budget; n++ {
		found := true
		for _, pod := range []string{"a", "b"} {
			glog.Infof("Checking access log of %s\n", pod)
			access := podLogs(pods[pod], "proxy")
			if strings.Contains(access, "segmentation fault") {
				return fmt.Errorf("segmentation fault in proxy %s", pod)
			}
			if strings.Contains(access, "assert failure") {
				return fmt.Errorf("assert failure in proxy %s", pod)
			}
			for _, id := range r.accessLogs[pod] {
				if !strings.Contains(access, id) {
					glog.Infof("Failed to find request id %s in log of %s\n", id, pod)
					found = false
					break
				}
			}
			if !found {
				break
			}
		}

		if found {
			return nil
		}

		time.Sleep(time.Second)
	}
	return fmt.Errorf("exceeded budget for checking access logs")
}

func (r *reachability) checkMixerLogs() error {
	glog.Info("Checking mixer logs for request IDs...")

	for n := 0; n < budget; n++ {
		found := true
		access := podLogs(pods["mixer"], "mixer")
		for _, pod := range []string{"a", "b"} {
			for _, id := range r.accessLogs[pod] {
				if !strings.Contains(access, id) {
					glog.Infof("Failed to find request id %s in mixer logs\n", id)
					found = false
					break
				}
			}
			if !found {
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

func (r *reachability) makeTCPRequest(src, dst, port, domain string, done func() bool) func() error {
	return func() error {
		url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
		for n := 0; n < budget; n++ {
			glog.Infof("Making a request %s from %s (attempt %d)...\n", url, src, n)
			request, err := shell(fmt.Sprintf("kubectl exec %s -n %s -c app -- client -url %s",
				pods[src], params.namespace, url))
			if err != nil {
				return err
			}
			match := regexp.MustCompile("StatusCode=(.*)").FindStringSubmatch(request)
			if len(match) > 1 && match[1] == "200" {
				return nil
			}
			if done() {
				return nil
			}
		}
		return fmt.Errorf("failed to verify TCP routing from %s to %s (url %s)", src, dst, url)
	}
}

func (r *reachability) verifyTCPRouting() error {
	g, ctx := errgroup.WithContext(context.Background())
	testPods := []string{"a", "b", "t"}
	for _, src := range testPods {
		for _, dst := range testPods {
			for _, port := range []string{":90", ":9090"} {
				for _, domain := range []string{"", "." + params.namespace} {
					if params.parallel {
						g.Go(r.makeTCPRequest(src, dst, port, domain, func() bool {
							select {
							case <-time.After(time.Second):
								// try again
							case <-ctx.Done():
								return true
							}
							return false

						}))
					} else {
						if err := r.makeTCPRequest(src, dst, port, domain, func() bool { return false })(); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	if params.parallel {
		if err := g.Wait(); err != nil {
			return err
		}
	}
	return nil
}
