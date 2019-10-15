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

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"

	"istio.io/istio/istioctl/pkg/kubernetes"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

var (
	forDistribution bool
	targetResource  string
	forDelete       bool
	threshold       float32
	timeout         time.Duration
	resourceVersion string
	//resourceVersions []string
	rvLock sync.Mutex
)

const pollInterval = time.Second

// waitCmd represents the wait command
func waitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wait [flags] <target-resource>",
		Short: "Wait for an Istio Resource",
		Long: `Waits for the specified condition to be true of an istio resource.  For example:

istioctl experimental wait --for-distribution virtual-service/default/bookinfo

will block until the bookinfo virtual service has been distributed to all proxies in the mesh.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if forDelete {
				return errors.New("wait for delete is not yet implemented")
			} else if !forDistribution {
				return errors.New("one of for-delete and for-distribution must be specified")
			}
			var versionChan chan string
			var g *errgroup.Group
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if resourceVersion == "" {
				versionChan, g = getAndWatchResource(ctx, targetResource) // setup version getter from kubernetes
			} else {
				versionChan = make(chan string, 1)
				versionChan <- resourceVersion
			}
			// wait for all deployed versions to be contained in resourceVersions
			t := time.NewTicker(pollInterval)
			resourceVersions := make([]string, 1)
			for {
				select {
				case newVersion := <-versionChan:
					resourceVersions = append(resourceVersions, newVersion)
				case <-t.C:
					finished, err := poll(resourceVersions)
					if err != nil {
						return err
					} else if finished {
						return nil
					}
				case <-ctx.Done():
					// I think this means the timeout has happened:
					t.Stop()
					if err := g.Wait(); err != nil {
						return err
					}
					return errors.New("timeout expired before resource was distributed")
				}
			}
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			targetResource = args[0]
			return parseResource(targetResource)
		},
	}
	cmd.PersistentFlags().BoolVar(&forDistribution, "for-distribution", false,
		"wait for the designated resource to be distributed")
	cmd.PersistentFlags().BoolVar(&forDelete, "for-delete", false,
		"wait for the designated resource to be distributed")
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", time.Second*30,
		"the duration to wait before failing (default 30s)")
	cmd.PersistentFlags().Float32Var(&threshold, "threshold", 1,
		"The ratio of distribution required for success (default 1.0)")
	cmd.PersistentFlags().StringVar(&resourceVersion, "resource-version", "",
		"Causes istioctl to wait for a specific version of config to become current, rather than using whatever is latest in k8s")
	return cmd
}

func parseResource(input string) error {
	return nil
}

func countVersions(versionCount map[string]int, configVersion string) {
	if count, ok := versionCount[configVersion]; ok {
		versionCount[configVersion] = count + 1
	} else {
		versionCount[configVersion] = 1
	}
}

func poll(acceptedVersions []string) (bool, error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return false, err
	}
	path := fmt.Sprintf("/debug/config_distribution?id=%s", targetResource)
	pilotResponses, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", path, nil)
	if err != nil {
		return false, err
	}
	versionCount := make(map[string]int)
	for _, response := range pilotResponses {
		var configVersions []v2.SyncedVersions
		err = json.Unmarshal(response, &configVersions)
		if err != nil {
			return false, err
		}
		for _, configVersion := range configVersions {
			countVersions(versionCount, configVersion.ClusterVersion)
			countVersions(versionCount, configVersion.RouteVersion)
			countVersions(versionCount, configVersion.ListenerVersion)
		}
	}

	finished := true
	for version, _ := range versionCount {
		if !contains(acceptedVersions, version) {
			finished = false
			break
		}
	}
	return finished, nil
}

// getAndWatchResource ensures that ResourceVersions always contains
// the current resourceVersion of the targetResource, adding new versions
// as they are created.
func getAndWatchResource(ictx context.Context, targetResource string) (chan string, *errgroup.Group) {
	result := make(chan string, 1)
	g, ctx := errgroup.WithContext(ictx)
	g.Go(func() error {
		// retrieve resource version from Kubernetes
		client, err := kubernetes.NewClient("", "")
		if err != nil {
			return err
		}
		req := client.Get().AbsPath(targetResource)
		res := req.Do()
		if res.Error() != nil {
			return res.Error()
		}
		obj, err := res.Get()
		if err != nil {
			return err
		}
		metaAccessor := meta.NewAccessor()
		resourceVersion, err = metaAccessor.ResourceVersion(obj)
		if err != nil {
			return err
		}
		result <- resourceVersion
		watch, err := req.Watch()
		if err != nil {
			return err
		}
		for w := range watch.ResultChan() {
			newVersion, err := metaAccessor.ResourceVersion(w.Object)
			if err != nil {
				return err
			}
			result <- newVersion
			select {
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})
	return result, g
}
