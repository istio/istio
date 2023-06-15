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

package metrics

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

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/dashboard"
	"istio.io/istio/pkg/log"
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

func Cmd(ctx cli.Context) *cobra.Command {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, ctx, args)
		},
		DisableFlagsInUseLine: true,
	}

	cmd.PersistentFlags().DurationVarP(&metricsDuration, "duration", "d", time.Minute, "Duration of query metrics, default value is 1m.")

	return cmd
}

type workloadMetrics struct {
	workload                           string
	totalRPS, errorRPS                 float64
	p50Latency, p90Latency, p99Latency time.Duration
}

func run(c *cobra.Command, ctx cli.Context, args []string) error {
	log.Debugf("metrics command invoked for workload(s): %v", args)

	client, err := ctx.CLIClientWithRevision(metricsOpts.Revision)
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %v", err)
	}

	pl, err := client.PodsForSelector(context.TODO(), ctx.IstioNamespace(), "app=prometheus")
	if err != nil {
		return fmt.Errorf("not able to locate Prometheus pod: %v", err)
	}

	if len(pl.Items) < 1 {
		return errors.New("no Prometheus pods found")
	}

	// only use the first pod in the list
	promPod := pl.Items[0]
	fw, err := client.NewPortForwarder(promPod.Name, ctx.IstioNamespace(), "", 0, 9090)
	if err != nil {
		return fmt.Errorf("could not build port forwarder for prometheus: %v", err)
	}

	if err = fw.Start(); err != nil {
		return fmt.Errorf("failure running port forward process: %v", err)
	}

	// Close the forwarder either when we exit or when an this processes is interrupted.
	defer fw.Close()
	dashboard.ClosePortForwarderOnInterrupt(fw)

	log.Debugf("port-forward to prometheus pod ready")

	promAPI, err := prometheusAPI(fmt.Sprintf("http://%s", fw.Address()))
	if err != nil {
		return fmt.Errorf("failure running port forward process: %v", err)
	}

	printHeader(c.OutOrStdout())

	workloads := args
	for _, workload := range workloads {
		sm, err := metrics(promAPI, workload, metricsDuration)
		if err != nil {
			return fmt.Errorf("could not build metrics for workload '%s': %v", workload, err)
		}

		printMetrics(c.OutOrStdout(), sm)
	}
	return nil
}

func prometheusAPI(address string) (promv1.API, error) {
	promClient, err := api.NewClient(api.Config{Address: address})
	if err != nil {
		return nil, fmt.Errorf("could not build prometheus client: %v", err)
	}
	return promv1.NewAPI(promClient), nil
}

func metrics(promAPI promv1.API, workload string, duration time.Duration) (workloadMetrics, error) {
	parts := strings.Split(workload, ".")
	wname := parts[0]
	wns := ""
	if len(parts) > 1 {
		wns = parts[1]
	}

	rpsQuery := fmt.Sprintf(`sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[%s]))`,
		reqTot, destWorkloadLabel, wname, destWorkloadNamespaceLabel, wns, duration)
	errRPSQuery := fmt.Sprintf(`sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination",response_code=~"[45][0-9]{2}"}[%s]))`,
		reqTot, destWorkloadLabel, wname, destWorkloadNamespaceLabel, wns, duration)

	var me *multierror.Error
	var err error
	sm := workloadMetrics{workload: workload}
	sm.totalRPS, err = vectorValue(promAPI, rpsQuery)
	if err != nil {
		me = multierror.Append(me, err)
	}

	sm.errorRPS, err = vectorValue(promAPI, errRPSQuery)
	if err != nil {
		me = multierror.Append(me, err)
	}

	p50Latency, err := getLatency(promAPI, wname, wns, duration, 0.5)
	if err != nil {
		me = multierror.Append(me, err)
	}
	sm.p50Latency = p50Latency

	p90Latency, err := getLatency(promAPI, wname, wns, duration, 0.9)
	if err != nil {
		me = multierror.Append(me, err)
	}
	sm.p90Latency = p90Latency

	p99Latency, err := getLatency(promAPI, wname, wns, duration, 0.99)
	if err != nil {
		me = multierror.Append(me, err)
	}
	sm.p99Latency = p99Latency

	if me.ErrorOrNil() != nil {
		return sm, fmt.Errorf("error retrieving some metrics: %v", me.Error())
	}

	return sm, nil
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

func printMetrics(writer io.Writer, wm workloadMetrics) {
	w := tabwriter.NewWriter(writer, 13, 1, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintf(w, "%40s\t", wm.workload)
	_, _ = fmt.Fprintf(w, "%.3f\t", wm.totalRPS)
	_, _ = fmt.Fprintf(w, "%.3f\t", wm.errorRPS)
	_, _ = fmt.Fprintf(w, "%s\t", wm.p50Latency)
	_, _ = fmt.Fprintf(w, "%s\t", wm.p90Latency)
	_, _ = fmt.Fprintf(w, "%s\t\n", wm.p99Latency)
	_ = w.Flush()
}
