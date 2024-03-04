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

package wait

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/slices"
)

var (
	forFlag      string
	nameflag     string
	proxyFlag    string
	threshold    float32
	timeout      time.Duration
	generation   string
	verbose      bool
	targetSchema resource.Schema
)

const pollInterval = time.Second

// Cmd represents the wait command
func Cmd(cliCtx cli.Context) *cobra.Command {
	namespace := cliCtx.Namespace()
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "wait [flags] <type> <name>[.<namespace>]",
		Short: "Wait for an Istio resource",
		Long:  `Waits for the specified condition to be true of an Istio resource.`,
		Example: `  # Wait until the bookinfo virtual service has been distributed to all proxies in the mesh
  istioctl experimental wait --for=distribution virtualservice bookinfo.default

  # Wait until the bookinfo virtual service has been distributed to a specific proxy
  istioctl experimental wait --for=distribution virtualservice bookinfo.default --proxy workload-instance.namespace

  # Wait until 99% of the proxies receive the distribution, timing out after 5 minutes
  istioctl experimental wait --for=distribution --threshold=.99 --timeout=300s virtualservice bookinfo.default
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if forFlag == "delete" {
				return errors.New("wait for delete is not yet implemented")
			} else if forFlag != "distribution" {
				return fmt.Errorf("--for must be 'delete' or 'distribution', got: %s", forFlag)
			}
			if proxyFlag != "" && threshold != 1 {
				printVerbosef(cmd, "both the proxy and threshold options were provided; the threshold option is being ignored.")
				threshold = 1
			}
			var w *watcher
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if generation == "" {
				w = getAndWatchResource(ctx, cliCtx) // setup version getter from kubernetes
			} else {
				w = withContext(ctx)
				w.Go(func(result chan string) error {
					result <- generation
					return nil
				})
			}
			// wait for all deployed versions to be contained in generations
			t := time.NewTicker(pollInterval)
			printVerbosef(cmd, "getting first version from chan")
			firstVersion, err := w.BlockingRead()
			if err != nil {
				return fmt.Errorf("unable to retrieve Kubernetes resource %s: %v", "", err)
			}
			generations := []string{firstVersion}
			targetResource := config.Key(
				targetSchema.Group(), targetSchema.Version(), targetSchema.Kind(),
				nameflag, namespace)
			for {
				// run the check here as soon as we start
				// because tickers won't run immediately
				present, notpresent, sdcnum, err := poll(cliCtx, cmd, generations, targetResource, proxyFlag, opts)
				printVerbosef(cmd, "Received poll result: %d/%d", present, present+notpresent)
				if err != nil {
					return err
				} else if float32(present)/float32(present+notpresent) >= threshold {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Resource %s present on %d out of %d configurations across %d sidecars\n",
						targetResource, present, present+notpresent, sdcnum)
					return nil
				}
				select {
				case newVersion := <-w.resultsChan:
					printVerbosef(cmd, "received new target version: %s", newVersion)
					generations = append(generations, newVersion)
				case <-t.C:
					printVerbosef(cmd, "tick")
					continue
				case err = <-w.errorChan:
					t.Stop()
					return fmt.Errorf("unable to retrieve Kubernetes resource2 %s: %v", "", err)
				case <-ctx.Done():
					printVerbosef(cmd, "timeout")
					// I think this means the timeout has happened:
					t.Stop()
					errTmpl := "timeout expired before resource %s became effective on %s"
					var errMsg string
					if proxyFlag != "" {
						errMsg = fmt.Sprintf(errTmpl, targetResource, proxyFlag)
					} else {
						errMsg = fmt.Sprintf(errTmpl, targetResource, "all sidecars")
					}
					return fmt.Errorf(errMsg)
				}
			}
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return err
			}
			nameflag, namespace = handlers.InferPodInfo(args[1], cliCtx.NamespaceOrDefault(namespace))
			return validateType(args[0])
		},
	}
	cmd.PersistentFlags().StringVar(&forFlag, "for", "distribution",
		"Wait condition, must be 'distribution' or 'delete'")
	cmd.PersistentFlags().StringVar(&proxyFlag, "proxy", "",
		"Name of a specific proxy to wait for the condition to be satisfied")
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", time.Second*30,
		"The duration to wait before failing")
	cmd.PersistentFlags().Float32Var(&threshold, "threshold", 1,
		"The ratio of distribution required for success")
	cmd.PersistentFlags().StringVar(&generation, "generation", "",
		"Wait for a specific generation of config to become current, rather than using whatever is latest in "+
			"Kubernetes")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enables verbose output")
	_ = cmd.PersistentFlags().MarkHidden("verbose")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func printVerbosef(cmd *cobra.Command, template string, args ...any) {
	if verbose {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), template+"\n", args...)
	}
}

func validateType(kind string) error {
	originalKind := kind

	// Remove any dashes.
	kind = strings.ReplaceAll(kind, "-", "")

	for _, s := range collections.Pilot.All() {
		if strings.EqualFold(kind, s.Kind()) {
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

const distributionTrackingDisabledErrorString = "pilot version tracking is disabled " +
	"(To enable this feature, please set PILOT_ENABLE_CONFIG_DISTRIBUTION_TRACKING=true)"

func poll(ctx cli.Context,
	cmd *cobra.Command,
	acceptedVersions []string,
	targetResource string,
	proxyID string,
	opts clioptions.ControlPlaneOptions,
) (present, notpresent, sdcnum int, err error) {
	kubeClient, err := ctx.CLIClientWithRevision(opts.Revision)
	if err != nil {
		return 0, 0, 0, err
	}
	path := fmt.Sprintf("debug/config_distribution?resource=%s", targetResource)
	pilotResponses, err := kubeClient.AllDiscoveryDo(context.TODO(), ctx.IstioNamespace(), path)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("unable to query pilot for distribution "+
			"(are you using pilot version >= 1.4 with config distribution tracking on): %s", err)
	}
	sdcnum = 0
	versionCount := make(map[string]int)
	for _, response := range pilotResponses {
		var configVersions []xds.SyncedVersions
		err = json.Unmarshal(response, &configVersions)
		if err != nil {
			respStr := string(response)
			if strings.Contains(respStr, xds.DistributionTrackingDisabledMessage) {
				return 0, 0, 0, fmt.Errorf("%s", distributionTrackingDisabledErrorString)
			}
			return 0, 0, 0, err
		}
		printVerbosef(cmd, "sync status: %+v", configVersions)
		sdcnum += len(configVersions)
		for _, configVersion := range configVersions {
			if proxyID != "" && configVersion.ProxyID != proxyID {
				continue
			}
			countVersions(versionCount, configVersion.ClusterVersion)
			countVersions(versionCount, configVersion.RouteVersion)
			countVersions(versionCount, configVersion.ListenerVersion)
			countVersions(versionCount, configVersion.EndpointVersion)
		}
	}

	for version, count := range versionCount {
		if slices.Contains(acceptedVersions, version) {
			present += count
		} else {
			notpresent += count
		}
	}
	return present, notpresent, sdcnum, nil
}

// getAndWatchResource ensures that Generations always contains
// the current generation of the targetResource, adding new versions
// as they are created.
func getAndWatchResource(ictx context.Context, cliCtx cli.Context) *watcher {
	g := withContext(ictx)
	// copy nameflag to avoid race
	nf := nameflag
	g.Go(func(result chan string) error {
		// retrieve latest generation from Kubernetes
		kc, err := cliCtx.CLIClient()
		if err != nil {
			return err
		}
		dclient := kc.Dynamic()
		r := dclient.Resource(targetSchema.GroupVersionResource()).Namespace(cliCtx.Namespace())
		watch, err := r.Watch(context.TODO(), metav1.ListOptions{FieldSelector: "metadata.name=" + nf})
		if err != nil {
			return err
		}
		for w := range watch.ResultChan() {
			o, ok := w.Object.(metav1.Object)
			if !ok {
				continue
			}
			if o.GetName() == nf {
				result <- strconv.FormatInt(o.GetGeneration(), 10)
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
