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
	"time"

	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/config/schemas"

	"istio.io/istio/pilot/pkg/model"

	"golang.org/x/sync/errgroup"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"

	"istio.io/istio/istioctl/pkg/kubernetes"
	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

var (
	forFlag         string
	typ, nameflag   string
	threshold       float32
	timeout         time.Duration
	resourceVersion string
)

const pollInterval = time.Second

// waitCmd represents the wait command
func waitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wait [flags] <type> <name>[.<namespace>]",
		Short: "Wait for an Istio resource",
		Long: `Waits for the specified condition to be true of an Istio resource.  For example:

istioctl experimental wait --for-distribution virtual-service/default/bookinfo

will block until the bookinfo virtual service has been distributed to all proxies in the mesh.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if forFlag == "delete" {
				return errors.New("wait for delete is not yet implemented")
			} else if forFlag != "distribution" {
				return fmt.Errorf("--for must be 'delete' or 'distribution', got: %s", forFlag)
			}
			var versionChan chan string
			g := &errgroup.Group{}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			targetResource := model.Key(typ, nameflag, namespace)
			if resourceVersion == "" {
				versionChan, g = getAndWatchResource(ctx, targetResource) // setup version getter from kubernetes
			} else {
				versionChan = make(chan string, 1)
				versionChan <- resourceVersion
			}
			// wait for all deployed versions to be contained in resourceVersions
			t := time.NewTicker(pollInterval)
			resourceVersions := []string{<-versionChan}
			for {
				//run the check here as soon as we start
				// because tickers wont' run immediately
				present, notpresent, err := poll(resourceVersions, targetResource)
				if err != nil {
					return err
				} else if float32(present)/float32(present+notpresent) >= threshold {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Resource %s present on %d out of %d sidecars",
						targetResource, present, present+notpresent)
					return nil
				}
				select {
				case newVersion := <-versionChan:
					resourceVersions = append(resourceVersions, newVersion)
				case <-t.C:
					continue
				case <-ctx.Done():
					// I think this means the timeout has happened:
					t.Stop()
					if err := g.Wait(); err != nil {
						return err
					}
					return fmt.Errorf("timeout expired before resource %s became effective on all sidecars",
						targetResource)
				}
			}
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return err
			}
			typ = args[0]
			nameflag, namespace = handlers.InferPodInfo(args[1], handlers.HandleNamespace(namespace, defaultNamespace))
			return validateType(&typ)
		},
	}
	cmd.PersistentFlags().StringVar(&forFlag, "for", "distribution",
		"wait condition, must be 'distribution' or 'delete'")
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", time.Second*30,
		"the duration to wait before failing (default 30s)")
	cmd.PersistentFlags().Float32Var(&threshold, "threshold", 1,
		"the ratio of distribution required for success (default 1.0)")
	cmd.PersistentFlags().StringVar(&resourceVersion, "resource-version", "",
		"wait for a specific version of config to become current, rather than using whatever is latest in "+
			"kubernetes")
	return cmd
}

func validateType(typ *string) error {
	for _, instance := range schemas.Istio {
		if *typ == instance.VariableName {
			*typ = instance.Type
		}
		if *typ == instance.Type {
			return nil
		}
	}
	return fmt.Errorf("type %s is not recognized", *typ)
}

func countVersions(versionCount map[string]int, configVersion string) {
	if count, ok := versionCount[configVersion]; ok {
		versionCount[configVersion] = count + 1
	} else {
		versionCount[configVersion] = 1
	}
}

func poll(acceptedVersions []string, targetResource string) (present, notpresent int, err error) {
	kubeClient, err := clientExecFactory(kubeconfig, configContext)
	if err != nil {
		return 0, 0, err
	}
	path := fmt.Sprintf("/debug/config_distribution?resource=%s", targetResource)
	pilotResponses, err := kubeClient.AllPilotsDiscoveryDo(istioNamespace, "GET", path, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("unable to query pilot for distribution "+
			"(are you using pilot version >= 1.4 with config distribution tracking on): %s", err)
	}
	versionCount := make(map[string]int)
	for _, response := range pilotResponses {
		var configVersions []v2.SyncedVersions
		err = json.Unmarshal(response, &configVersions)
		if err != nil {
			return 0, 0, err
		}
		for _, configVersion := range configVersions {
			countVersions(versionCount, configVersion.ClusterVersion)
			countVersions(versionCount, configVersion.RouteVersion)
			countVersions(versionCount, configVersion.ListenerVersion)
		}
	}

	for version := range versionCount {
		if contains(acceptedVersions, version) {
			present++
		} else {
			notpresent++
		}
	}
	return present, notpresent, nil
}

// getAndWatchResource ensures that ResourceVersions always contains
// the current resourceVersion of the targetResource, adding new versions
// as they are created.
func getAndWatchResource(ictx context.Context, targetResource string) (chan string, *errgroup.Group) {
	result := make(chan string, 1)
	g, ctx := errgroup.WithContext(ictx)
	g.Go(func() error {
		// retrieve resource version from Kubernetes
		client, err := kubernetes.NewClient(kubeconfig, configContext)
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
			default:
				continue
			}
		}

		return nil
	})
	return result, g
}
