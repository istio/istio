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
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/pkg/log"
)

var (
	metricsOpts     clioptions.ControlPlaneOptions
	metricsDuration time.Duration
)

const (
	destWorkloadLabel          = "destination_workload"
	destWorkloadNamespaceLabel = "destination_workload_namespace"
	reqTot                     = "istio_requests_total"
	reqDur                     = "istio_request_duration_milliseconds"
)

func metricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics <workload name>...",
		Short: "Prints the metrics for the specified workload(s) when running in Kubernetes.",
		Long: `
Prints the metrics for the specified service(s) when running in Kubernetes.

This command finds a Prometheus pod running in the specified istio system
namespace. It then executes a series of queries per requested workload to
find the following top-level workload metrics: total requests per second,
error rate, and request latency at p50, p90, and p99 percentiles. The
query results are printed to the console, organized by workload name.

All metrics returned are from server-side reports. This means that latencies
and error rates are from the perspective of the service itself and not of an
individual client (or aggregate set of clients). Rates and latencies are
calculated over a time interval of 1 minute.
`,
		Example: `  # Retrieve workload metrics for productpage-v1 workload
  istioctl experimental metrics productpage-v1

  # Retrieve resource metrics by their type and name, support type [workload]
  istioctl experimental metrics workload/productpage-v1

  # Retrieve workload metrics for various services with custom duration
  istioctl experimental metrics productpage-v1 -d 2m

  # Retrieve workload metrics for various services in the different namespaces
  istioctl experimental metrics productpage-v1.foo reviews-v1.bar ratings-v1.baz`,
		// nolint: goimports
		Aliases: []string{"m"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("metrics requires workload name")
			}
			return nil
		},
		RunE:                  run,
		DisableFlagsInUseLine: true,
	}

	cmd.PersistentFlags().DurationVarP(&metricsDuration, "duration", "d", time.Minute, "Duration of query metrics, default value is 1m.")

	return cmd
}

type resourceMetrics struct {
	resource                           resourceTuple
	totalRPS, errorRPS                 float64
	p50Latency, p90Latency, p99Latency time.Duration
}

type resourceType string

const (
	workloadResourceType resourceType = "workload"
)

func newResourceType(in string) (resourceType, error) {
	switch in {
	case "workload":
		return workloadResourceType, nil
	default:
		return "", fmt.Errorf("invalid resource type, support type [workload]")
	}
}

type resourceTuple struct {
	Type      resourceType
	Name      string
	Namespace string
}

// inferNsInfo Uses name to infer namespace if the passed name contains namespace information.
// Otherwise uses the namespace value passed into the function
func inferNsInfo(name, namespace string) (string, string) {
	separator := strings.LastIndex(name, ".")
	if separator < 0 {
		return name, namespace
	}

	return name[0:separator], name[separator+1:]
}

// SplitResourceArgument splits the argument with commas and returns unique
// strings in the original order.
func SplitResourceArgument(arg string) []string {
	out := []string{}
	set := sets.NewString()
	for _, s := range strings.Split(arg, ",") {
		if set.Has(s) {
			continue
		}
		set.Insert(s)
		out = append(out, s)
	}
	return out
}

func inferResourceTuple(name, specifyNS string, specifyRes resourceType) (resourceTuple, error) {
	resName, ns := inferNsInfo(name, specifyNS)
	if !strings.Contains(resName, "/") {
		return resourceTuple{specifyRes, resName, ns}, nil
	}

	seg := strings.Split(resName, "/")
	if len(seg) != 2 {
		return resourceTuple{}, fmt.Errorf("arguments in resource/name form may not have more than one slash")
	}

	resource, name := seg[0], seg[1]
	if len(resource) == 0 || len(name) == 0 || len(SplitResourceArgument(resource)) != 1 {
		return resourceTuple{}, fmt.Errorf("arguments in resource/name form must have a single resource and name")
	}
	rt, err := newResourceType(resource)
	if err != nil {
		return resourceTuple{}, err
	}
	return resourceTuple{rt, name, ns}, nil
}

func run(c *cobra.Command, args []string) error {
	// TODO to support multi resources(workload | service)
	log.Debugf("metrics command invoked for workload(s): %v", args)

	client, err := kubeClientWithRevision(kubeconfig, configContext, metricsOpts.Revision)
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %v", err)
	}

	pl, err := client.PodsForSelector(context.TODO(), istioNamespace, "app=prometheus")
	if err != nil {
		return fmt.Errorf("not able to locate Prometheus pod: %v", err)
	}

	if len(pl.Items) < 1 {
		return errors.New("no Prometheus pods found")
	}

	// only use the first pod in the list
	promPod := pl.Items[0]
	fw, err := client.NewPortForwarder(promPod.Name, istioNamespace, "", 0, 9090)
	if err != nil {
		return fmt.Errorf("could not build port forwarder for prometheus: %v", err)
	}

	if err = fw.Start(); err != nil {
		return fmt.Errorf("failure running port forward process: %v", err)
	}

	// Close the forwarder either when we exit or when an this processes is interrupted.
	defer fw.Close()
	closePortForwarderOnInterrupt(fw)

	log.Debugf("port-forward to prometheus pod ready")

	promAPI, err := prometheusAPI(fmt.Sprintf("http://%s", fw.Address()))
	if err != nil {
		return fmt.Errorf("failure running port forward process: %v", err)
	}

	printHeader(c.OutOrStdout())

	// infer resource tuple
	// TODO: feature specify Resource, Namespace
	specifyRes := workloadResourceType
	allErrs := []error{}
	for _, arg := range args {
		res, err := inferResourceTuple(arg, "", specifyRes)
		if err != nil {
			allErrs = append(allErrs, err)
			continue
		}
		sm, err := metrics(promAPI, res, metricsDuration)
		if err != nil {
			return fmt.Errorf("could not build metrics for workload '%s': %v", res.Name, err)
		}

		printMetrics(c.OutOrStdout(), sm)
	}

	return utilerrors.NewAggregate(allErrs)
}

func prometheusAPI(address string) (promv1.API, error) {
	promClient, err := api.NewClient(api.Config{Address: address})
	if err != nil {
		return nil, fmt.Errorf("could not build prometheus client: %v", err)
	}
	return promv1.NewAPI(promClient), nil
}

func metrics(promAPI promv1.API, res resourceTuple, duration time.Duration) (resourceMetrics, error) {
	if res.Type == workloadResourceType {
		rpsQuery := fmt.Sprintf(`sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[%s]))`,
			reqTot, destWorkloadLabel, res.Name, destWorkloadNamespaceLabel, res.Namespace, duration)
		errRPSQuery := fmt.Sprintf(`sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination",response_code=~"[45][0-9]{2}"}[%s]))`,
			reqTot, destWorkloadLabel, res.Name, destWorkloadNamespaceLabel, res.Namespace, duration)
		var me *multierror.Error
		var err error
		sm := resourceMetrics{resource: res}
		sm.totalRPS, err = vectorValue(promAPI, rpsQuery)
		if err != nil {
			me = multierror.Append(me, err)
		}

		sm.errorRPS, err = vectorValue(promAPI, errRPSQuery)
		if err != nil {
			me = multierror.Append(me, err)
		}

		p50Latency, err := getLatency(promAPI, res.Name, res.Namespace, duration, 0.5)
		if err != nil {
			me = multierror.Append(me, err)
		}
		sm.p50Latency = p50Latency

		p90Latency, err := getLatency(promAPI, res.Name, res.Namespace, duration, 0.9)
		if err != nil {
			me = multierror.Append(me, err)
		}
		sm.p90Latency = p90Latency

		p99Latency, err := getLatency(promAPI, res.Name, res.Namespace, duration, 0.99)
		if err != nil {
			me = multierror.Append(me, err)
		}
		sm.p99Latency = p99Latency

		if me.ErrorOrNil() != nil {
			return sm, fmt.Errorf("error retrieving some metrics: %v", me.Error())
		}

		return sm, nil
	}

	return resourceMetrics{}, fmt.Errorf("invalid resource type: %s", res.Type)
}

func getLatency(promAPI promv1.API, workloadName, workloadNamespace string, duration time.Duration, quantile float64) (time.Duration, error) {
	latencyQuery := fmt.Sprintf(`histogram_quantile(%f, sum(rate(%s_bucket{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[%s])) by (le))`,
		quantile, reqDur, destWorkloadLabel, workloadName, destWorkloadNamespaceLabel, workloadNamespace, duration)

	letency, err := vectorValue(promAPI, latencyQuery)
	if err != nil {
		return time.Duration(0), err
	}

	return convertLatencyToDuration(letency), nil
}

func vectorValue(promAPI promv1.API, query string) (float64, error) {
	val, _, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("query() failure for '%s': %v", query, err)
	}

	log.Debugf("executing query: %s  result:%s", query, val)

	switch v := val.(type) {
	case model.Vector:
		if v.Len() < 1 {
			log.Debugf("no values for query: %s", query)
			return 0, nil
		}

		return float64(v[0].Value), nil
	default:
		return 0, errors.New("bad metric value type returned for query")
	}
}

func convertLatencyToDuration(val float64) time.Duration {
	return time.Duration(val) * time.Millisecond
}

func printHeader(writer io.Writer) {
	w := tabwriter.NewWriter(writer, 13, 1, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintf(w, "%40s\tTOTAL RPS\tERROR RPS\tP50 LATENCY\tP90 LATENCY\tP99 LATENCY\t\n", "WORKLOAD")
	_ = w.Flush()
}

func printMetrics(writer io.Writer, rm resourceMetrics) {
	w := tabwriter.NewWriter(writer, 13, 1, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintf(w, "%40s\t", rm.resource.Name)
	_, _ = fmt.Fprintf(w, "%.3f\t", rm.totalRPS)
	_, _ = fmt.Fprintf(w, "%.3f\t", rm.errorRPS)
	_, _ = fmt.Fprintf(w, "%s\t", rm.p50Latency)
	_, _ = fmt.Fprintf(w, "%s\t", rm.p90Latency)
	_, _ = fmt.Fprintf(w, "%s\t\n", rm.p99Latency)
	_ = w.Flush()
}
