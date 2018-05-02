// Copyright 2018 Istio Authors
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

package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"istio.io/istio/pkg/log"
)

var (
	metricsCmd = &cobra.Command{
		Use:   "metrics <service name>...",
		Short: "Prints the metrics for the specified service(s) when running in Kubernetes.",
		Long: `
Prints the metrics for the specified service(s) when running in Kubernetes.

This command finds a Prometheus pod running in the specified istio system 
namespace. It then executes a series of queries per requested service to
find the following top-level service metrics: total requests per second,
error rate, and request latency at p50, p90, and p99 percentiles. The 
query results are printed to the console, organized by service name.

All metrics returned are from server-side reports. This means that latencies
and error rates are from the perspective of the service itself and not of an
individual client (or aggregate set of clients). Rates and latencies are
calculated over a time interval of 1 minute.
`,
		Example: `
# Retrieve service metrics for productpage service
istioctl experimental metrics productpage

# Retrieve service metrics for various services in the different namespaces
istioctl experimental metrics productpage.foo reviews.bar ratings.baz
`,
		Aliases: []string{"m"},
		Args:    cobra.MinimumNArgs(1),
		RunE:    run,
		DisableFlagsInUseLine: true,
	}
)

func init() {
	experimentalCmd.AddCommand(metricsCmd)
}

type serviceMetrics struct {
	service                            string
	totalRPS, errorRPS                 float64
	p50Latency, p90Latency, p99Latency time.Duration
}

func run(c *cobra.Command, args []string) error {
	log.Debugf("metrics command invoked for service(s): %v", args)

	config, _ := defaultRestConfig()
	client, err := createCoreV1Client()
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %v", err)
	}

	pl, err := prometheusPods(client)
	if err != nil {
		return fmt.Errorf("not able to locate prometheus pod: %v", err)
	}

	if len(pl.Items) < 1 {
		return errors.New("no prometheus pods found")
	}

	port, err := availablePort()
	if err != nil {
		return fmt.Errorf("not able to find available port: %v", err)
	}
	log.Debugf("Using local port: %d", port)

	// only use the first pod in the list
	promPod := pl.Items[0]
	fw, readyCh, err := buildPortForwarder(client, config, promPod.Name, port)
	if err != nil {
		return fmt.Errorf("could not build port forwarder for prometheus: %v", err)
	}

	errCh := make(chan error)
	go func() {
		errCh <- fw.ForwardPorts()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("failure running port forward process: %v", err)
	case <-readyCh:
		log.Debugf("port-forward to prometheus pod ready")
		defer fw.Close()

		promAPI, err := prometheusAPI(port)
		if err != nil {
			return err
		}

		printHeader()

		services := args
		for _, service := range services {
			sm, err := metrics(promAPI, service)
			if err != nil {
				return fmt.Errorf("could not build metrics for service '%s': %v", service, err)
			}

			printMetrics(sm)
		}
		return nil
	}
}

func prometheusPods(client cache.Getter) (*v1.PodList, error) {
	podGet := client.Get().Resource("pods").Namespace(istioNamespace).Param("labelSelector", "app=prometheus")
	obj, err := podGet.Do().Get()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving pod: %v", err)
	}
	return obj.(*v1.PodList), nil
}

func buildPortForwarder(client *rest.RESTClient, config *rest.Config, podName string, port int) (*portforward.PortForwarder, <-chan struct{}, error) {
	req := client.Post().Resource("pods").Namespace(istioNamespace).Name(podName).SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failure creating roundtripper: %v", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	stop := make(chan struct{})
	ready := make(chan struct{})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:9090", port)}, stop, ready, ioutil.Discard, os.Stderr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed establishing port-forward: %v", err)
	}

	return fw, ready, nil
}

func availablePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", ":0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func prometheusAPI(port int) (promv1.API, error) {
	promClient, err := api.NewClient(api.Config{Address: fmt.Sprintf("http://localhost:%d", port)})
	if err != nil {
		return nil, fmt.Errorf("could not build prometheus client: %v", err)
	}
	return promv1.NewAPI(promClient), nil
}

func metrics(promAPI promv1.API, service string) (serviceMetrics, error) {
	rpsQuery := fmt.Sprintf(`sum(rate(istio_request_count{destination_service=~"%s.*"}[1m]))`, service)
	errRPSQuery := fmt.Sprintf(`sum(rate(istio_request_count{destination_service=~"%s.*",response_code!="200"}[1m]))`, service)
	p50LatencyQuery := fmt.Sprintf(`histogram_quantile(%f, sum(rate(istio_request_duration_bucket{destination_service=~"%s.*"}[1m])) by (le))`, 0.5, service)
	p90LatencyQuery := fmt.Sprintf(`histogram_quantile(%f, sum(rate(istio_request_duration_bucket{destination_service=~"%s.*"}[1m])) by (le))`, 0.9, service)
	p99LatencyQuery := fmt.Sprintf(`histogram_quantile(%f, sum(rate(istio_request_duration_bucket{destination_service=~"%s.*"}[1m])) by (le))`, 0.99, service)

	var me *multierror.Error
	var err error
	sm := serviceMetrics{service: service}
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
	val, err := promAPI.Query(context.Background(), query, time.Now())
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

func printHeader() {
	w := tabwriter.NewWriter(os.Stdout, 13, 1, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "%13sSERVICE\tTOTAL RPS\tERROR RPS\tP50 LATENCY\tP90 LATENCY\tP99 LATENCY\t\n", "")
	w.Flush()
}

func printMetrics(sm serviceMetrics) {
	w := tabwriter.NewWriter(os.Stdout, 13, 1, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "%20s\t", sm.service)
	fmt.Fprintf(w, "%.3f\t", sm.totalRPS)
	fmt.Fprintf(w, "%.3f\t", sm.errorRPS)
	fmt.Fprintf(w, "%s\t", sm.p50Latency)
	fmt.Fprintf(w, "%s\t", sm.p90Latency)
	fmt.Fprintf(w, "%s\t\n", sm.p99Latency)
	w.Flush()
}
