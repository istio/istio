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

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
)

var (
	forFlag         string
	nameflag        string
	threshold       float32
	timeout         time.Duration
	resourceVersion string
	verbose         bool
	targetSchema    collection.Schema
	clientGetter    func(string, string) (dynamic.Interface, error)
)

const pollInterval = time.Second

// waitCmd represents the wait command
func waitCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "wait [flags] <type> <name>[.<namespace>]",
		Short: "Wait for an Istio resource",
		Long:  `Waits for the specified condition to be true of an Istio resource.`,
		Example: `
# Wait until the bookinfo virtual service has been distributed to all proxies in the mesh
istioctl experimental wait --for=distribution virtualservice bookinfo.default

# Wait until 99% of the proxies receive the distribution, timing out after 5 minutes
istioctl experimental wait --for=distribution --threshold=.99 --timeout=300 virtualservice bookinfo.default
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printVerbosef(cmd, "kubeconfig %s", kubeconfig)
			printVerbosef(cmd, "ctx %s", configContext)
			if forFlag == "delete" {
				return errors.New("wait for delete is not yet implemented")
			} else if forFlag != "distribution" {
				return fmt.Errorf("--for must be 'delete' or 'distribution', got: %s", forFlag)
			}
			var w *watcher
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if resourceVersion == "" {
				w = getAndWatchResource(ctx) // setup version getter from kubernetes
			} else {
				w = withContext(ctx)
				w.Go(func(result chan string) error {
					result <- resourceVersion
					return nil
				})
			}
			// wait for all deployed versions to be contained in resourceVersions
			t := time.NewTicker(pollInterval)
			printVerbosef(cmd, "getting first version from chan")
			firstVersion, err := w.BlockingRead()
			if err != nil {
				return fmt.Errorf("unable to retrieve Kubernetes resource %s: %v", "", err)
			}
			resourceVersions := []string{firstVersion}
			targetResource := model.Key(targetSchema.Resource().Kind(), nameflag, namespace)
			for {
				//run the check here as soon as we start
				// because tickers won't run immediately
				present, notpresent, err := poll(resourceVersions, targetResource, opts)
				printVerbosef(cmd, "Received poll result: %d/%d", present, present+notpresent)
				if err != nil {
					return err
				} else if float32(present)/float32(present+notpresent) >= threshold {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Resource %s present on %d out of %d sidecars\n",
						targetResource, present, present+notpresent)
					return nil
				}
				select {
				case newVersion := <-w.resultsChan:
					printVerbosef(cmd, "received new target version: %s", newVersion)
					resourceVersions = append(resourceVersions, newVersion)
				case <-t.C:
					printVerbosef(cmd, "tick")
					continue
				case err = <-w.errorChan:
					return fmt.Errorf("unable to retrieve Kubernetes resource %s: %v", "", err)
				case <-ctx.Done():
					printVerbosef(cmd, "timeout")
					// I think this means the timeout has happened:
					t.Stop()
					return fmt.Errorf("timeout expired before resource %s became effective on all sidecars",
						targetResource)
				}
			}
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return err
			}
			nameflag, namespace = handlers.InferPodInfo(args[1], handlers.HandleNamespace(namespace, defaultNamespace))
			return validateType(args[0])
		},
	}
	cmd.PersistentFlags().StringVar(&forFlag, "for", "distribution",
		"wait condition, must be 'distribution' or 'delete'")
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", time.Second*30,
		"the duration to wait before failing")
	cmd.PersistentFlags().Float32Var(&threshold, "threshold", 1,
		"the ratio of distribution required for success")
	cmd.PersistentFlags().StringVar(&resourceVersion, "resource-version", "",
		"wait for a specific version of config to become current, rather than using whatever is latest in "+
			"kubernetes")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enables verbose output")
	_ = cmd.PersistentFlags().MarkHidden("verbose")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func printVerbosef(cmd *cobra.Command, template string, args ...interface{}) {
	if verbose {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), template+"\n", args...)
	}
}

func validateType(kind string) error {
	originalKind := kind

	// Remove any dashes.
	kind = strings.ReplaceAll(kind, "-", "")

	for _, s := range collections.Pilot.All() {
		if strings.EqualFold(kind, s.Resource().Kind()) {
			targetSchema = s
			return nil
		}
	}
	return fmt.Errorf("type %s is not recognized", originalKind)
}

func countVersions(versionCount map[string]int, configVersion string) {
	if count, ok := versionCount[configVersion]; ok {
		versionCount[configVersion] = count + 1
	} else {
		versionCount[configVersion] = 1
	}
}

func poll(acceptedVersions []string, targetResource string, opts clioptions.ControlPlaneOptions) (present, notpresent int, err error) {
	kubeClient, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
	if err != nil {
		return 0, 0, err
	}
	path := fmt.Sprintf("/debug/config_distribution?resource=%s", targetResource)
	pilotResponses, err := kubeClient.AllDiscoveryDo(context.TODO(), istioNamespace, path)
	if err != nil {
		return 0, 0, fmt.Errorf("unable to query pilot for distribution "+
			"(are you using pilot version >= 1.4 with config distribution tracking on): %s", err)
	}
	versionCount := make(map[string]int)
	for _, response := range pilotResponses {
		var configVersions []xds.SyncedVersions
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

	for version, count := range versionCount {
		if contains(acceptedVersions, version) {
			present += count
		} else {
			notpresent += count
		}
	}
	return present, notpresent, nil
}

func init() {
	clientGetter = func(kubeconfig, context string) (dynamic.Interface, error) {
		config, err := kube.DefaultRestConfig(kubeconfig, context)
		if err != nil {
			return nil, err
		}
		cfg := dynamic.ConfigFor(config)
		dclient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, err
		}
		return dclient, nil
	}

}

// getAndWatchResource ensures that ResourceVersions always contains
// the current resourceVersion of the targetResource, adding new versions
// as they are created.
func getAndWatchResource(ictx context.Context) *watcher {
	g := withContext(ictx)
	g.Go(func(result chan string) error {
		// retrieve resource version from Kubernetes
		dclient, err := clientGetter(kubeconfig, configContext)
		if err != nil {
			return err
		}
		collectionParts := strings.Split(targetSchema.Name().String(), "/")
		group := targetSchema.Resource().Group()
		version := targetSchema.Resource().Version()
		resource := collectionParts[3]
		r := dclient.Resource(schema.GroupVersionResource{Group: group, Version: version, Resource: resource}).Namespace(namespace)
		obj, err := r.Get(context.TODO(), nameflag, metav1.GetOptions{})
		if err != nil {
			return err
		}
		localResourceVersion := obj.GetResourceVersion()
		result <- localResourceVersion
		watch, err := r.Watch(context.TODO(), metav1.ListOptions{ResourceVersion: localResourceVersion})
		if err != nil {
			return err
		}
		metaAccessor := meta.NewAccessor()
		for w := range watch.ResultChan() {
			watchname, err := metaAccessor.Name(w.Object)
			if err != nil {
				return err
			}
			if watchname == nameflag {
				newVersion, err := metaAccessor.ResourceVersion(w.Object)
				if err != nil {
					return err
				}
				result <- newVersion
			}
			select {
			case <-ictx.Done():
				return ictx.Err()
			default:
				continue
			}
		}

		return nil
	})
	return g
}

type watcher struct {
	resultsChan chan string
	errorChan   chan error
	ctx         context.Context
}

func withContext(ctx context.Context) *watcher {
	return &watcher{
		resultsChan: make(chan string, 1),
		errorChan:   make(chan error, 1),
		ctx:         ctx,
	}
}

func (w *watcher) Go(f func(chan string) error) {
	go func() {
		if err := f(w.resultsChan); err != nil {
			w.errorChan <- err
		}
	}()
}

func (w *watcher) BlockingRead() (string, error) {
	select {
	case err := <-w.errorChan:
		return "", err
	case res := <-w.resultsChan:
		return res, nil
	case <-w.ctx.Done():
		return "", w.ctx.Err()
	}
}
