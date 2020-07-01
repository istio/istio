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

	multierror "github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"

	"istio.io/pkg/log"

	"istio.io/istio/istioctl/pkg/clioptions"
)

var (
	metricsOpts clioptions.ControlPlaneOptions
	metricsCmd  = &cobra.Command{
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
		Example: `
# Retrieve workload metrics for productpage-v1 workload
istioctl experimental metrics productpage-v1

# Retrieve workload metrics for various services in the different namespaces
istioctl experimental metrics productpage-v1.foo reviews-v1.bar ratings-v1.baz
`,
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
)

const (
	wlabel   = "destination_workload"
	wnslabel = "destination_workload_namespace"
	reqTot   = "istio_requests_total"
	reqDur   = "istio_request_duration_seconds"
)

type workloadMetrics struct {
	workload                           string
	totalRPS, errorRPS                 float64
	p50Latency, p90Latency, p99Latency time.Duration
}

func run(c *cobra.Command, args []string) error {
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

	workloads := args
	for _, workload := range workloads {
		sm, err := metrics(promAPI, workload)
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

func metrics(promAPI promv1.API, workload string) (workloadMetrics, error) {

	parts := strings.Split(workload, ".")
	wname := parts[0]
	wns := ""
	if len(parts) > 1 {
		wns = parts[1]
	}

	rpsQuery := fmt.Sprintf(`sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[1m]))`, reqTot, wlabel, wname, wnslabel, wns)
	errRPSQuery := fmt.Sprintf(`sum(rate(%s{%s=~"%s.*", %s=~"%s.*",reporter="destination",response_code=~"[45][0-9]{2}"}[1m]))`,
		reqTot, wlabel, wname, wnslabel, wns)
	p50LatencyQuery := fmt.Sprintf(`histogram_quantile(%f, sum(rate(%s_bucket{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[1m])) by (le))`,
		0.5, reqDur, wlabel, wname, wnslabel, wns)
	p90LatencyQuery := fmt.Sprintf(`histogram_quantile(%f, sum(rate(%s_bucket{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[1m])) by (le))`,
		0.9, reqDur, wlabel, wname, wnslabel, wns)
	p99LatencyQuery := fmt.Sprintf(`histogram_quantile(%f, sum(rate(%s_bucket{%s=~"%s.*", %s=~"%s.*",reporter="destination"}[1m])) by (le))`,
		0.99, reqDur, wlabel, wname, wnslabel, wns)

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

	p50Latency, err := vectorValue(promAPI, p50LatencyQuery)
	if err != nil {
		me = multierror.Append(me, err)
	}
	sm.p50Latency = time.Duration(p50Latency*1000) * time.Millisecond

	p90Latency, err := vectorValue(promAPI, p90LatencyQuery)
	if err != nil {
		me = multierror.Append(me, err)
	}
	sm.p90Latency = time.Duration(p90Latency*1000) * time.Millisecond

	p99Latency, err := vectorValue(promAPI, p99LatencyQuery)
	if err != nil {
		me = multierror.Append(me, err)
	}
	sm.p99Latency = time.Duration(p99Latency*1000) * time.Millisecond

	if me.ErrorOrNil() != nil {
		return sm, fmt.Errorf("error retrieving some metrics: %v", me.Error())
	}

	return sm, nil
}

func vectorValue(promAPI promv1.API, query string) (float64, error) {
	log.Debugf("executing query: %s", query)
	val, _, err := promAPI.Query(context.Background(), query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("query() failure for '%s': %v", query, err)
	}

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

func printHeader(writer io.Writer) {
	w := tabwriter.NewWriter(writer, 13, 1, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "%40s\tTOTAL RPS\tERROR RPS\tP50 LATENCY\tP90 LATENCY\tP99 LATENCY\t\n", "WORKLOAD")
	_ = w.Flush()
}

func printMetrics(writer io.Writer, wm workloadMetrics) {
	w := tabwriter.NewWriter(writer, 13, 1, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "%40s\t", wm.workload)
	fmt.Fprintf(w, "%.3f\t", wm.totalRPS)
	fmt.Fprintf(w, "%.3f\t", wm.errorRPS)
	fmt.Fprintf(w, "%s\t", wm.p50Latency)
	fmt.Fprintf(w, "%s\t", wm.p90Latency)
	fmt.Fprintf(w, "%s\t\n", wm.p99Latency)
	_ = w.Flush()
}
