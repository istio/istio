// Copyright 2020 Istio Authors
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

package nothingupmysleeve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

func WaitForDistribution(ctx context.Context, namespace string, resourceName string, resourceType string, resourceVersion string,
	client rest.RESTClient, istioNamespace string, pollInterval time.Duration) error {
	// TODO: set up watch to add new versions to accepted versions
	// alternative: though this is not documented, k8s resource versions are almost always increasing ints.
	// changing the contains() check to a >= check, and that simplifies things.
	acceptedVersions := []string{resourceVersion}
	// get list of all pilots
	pilots, err := findAllPilotInstances(istioNamespace, client)
	if err != nil {
		return err
	}
	if len(pilots) < 1 {
		return errors.New("unable to find any Pilot instances")
	}

	// query each periodically, removing those that succeed.
	wg := sync.WaitGroup{}
	wg.Add(len(pilots))

	targetResource := fmt.Sprintf("%s/%s/%s", resourceType, namespace, resourceName)
	path := fmt.Sprintf("/debug/config_distribution?resource=%s", targetResource)
	var errs error

	for _, pilot := range pilots {
		go func() {
			t := time.NewTicker(pollInterval)
			for {
				// do the get here
				// port is 8080 for 1.6+
				response, err := proxyGet(pilot.Name, pilot.Namespace, path, 15014, client).DoRaw()
				if err != nil {
					// this might not be the right thing to do on error
					errs = multierror.Append(errs, err)
					break
				}
				var configVersions []SyncedVersions
				err = json.Unmarshal(response, &configVersions)
				if err != nil {
					// this might not be the right thing to do on error
					errs = multierror.Append(errs, err)
					break
				}
				versionCount := make(map[string]int)
				for _, configVersion := range configVersions {
					countVersions(versionCount, configVersion.ClusterVersion)
					countVersions(versionCount, configVersion.RouteVersion)
					countVersions(versionCount, configVersion.ListenerVersion)
				}

				notpresent := 0
				for version, count := range versionCount {
					if !contains(acceptedVersions, version) {
						notpresent += count
					}
				}
				if notpresent < 1 {
					// success!
					break
				} else {
					fmt.Printf("still waiting for %d proxies from pilot %s", notpresent, pilot.Name)
				}
				// repeat every interval until success or cancel
				select {
				case <-ctx.Done():
					errs = multierror.Append(errs, ctx.Err())
					break
				case <-t.C:
					continue
				}
			}
			wg.Done()
		}()
	}

	return nil
}

func findAllPilotInstances(istioNamespace string, client rest.RESTClient) ([]v1.Pod, error) {
	req := client.Get().
		Resource("pods").
		Namespace(istioNamespace)

	req.Param("labelSelector", "istio=pilot")
	req.Param("fieldSelector", "status.phase=Running")

	res := req.Do()
	if res.Error() != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", res.Error())
	}
	list := &v1.PodList{}
	if err := res.Into(list); err != nil {
		return nil, fmt.Errorf("unable to parse PodList: %v", res.Error())
	}
	return list.Items, nil
}

func proxyGet(name, namespace, path string, port int, client rest.RESTClient) rest.ResponseWrapper {
	pathURL, err := url.Parse(path)
	if err != nil {
		fmt.Printf("failed to parse path %s: %v", path, err)
		pathURL = &url.URL{Path: path}
	}
	request := client.Get().
		Namespace(namespace).
		Resource("pods").
		SubResource("proxy").
		Name(fmt.Sprintf("%s:%d", name, port)).
		Suffix(pathURL.Path)
	for key, vals := range pathURL.Query() {
		for _, val := range vals {
			request = request.Param(key, val)
		}
	}
	return request
}

// copied from istio.io/istio/pilot/pkg/proxy/envoy/v2/debug.go
// SyncedVersions shows what resourceVersion of a given resource has been acked by Envoy.
type SyncedVersions struct {
	ProxyID         string `json:"proxy,omitempty"`
	ClusterVersion  string `json:"cluster_acked,omitempty"`
	ListenerVersion string `json:"listener_acked,omitempty"`
	RouteVersion    string `json:"route_acked,omitempty"`
}

// copied from istio.io/istio/istioctl/cmd/wait.go
func countVersions(versionCount map[string]int, configVersion string) {
	if count, ok := versionCount[configVersion]; ok {
		versionCount[configVersion] = count + 1
	} else {
		versionCount[configVersion] = 1
	}
}

// copied from istio.io/istio/istioctl/cmd/wait.go
func contains(slice []string, s string) bool {
	for _, candidate := range slice {
		if candidate == s {
			return true
		}
	}

	return false
}
