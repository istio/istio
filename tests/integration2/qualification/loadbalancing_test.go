//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package qualification

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
)

const (
	testDuration = 1 * time.Minute
	numSendTasks = 16
	step         = time.Second * 15
	threshold    = 0.5
)

// NOTE: To avoid noise due to autoscaling, set the following flags to the same value (>1):
// --istio.test.kube.ingressGateway.replicaCount
// --istio.test.kube.ingressGateway.autoscaleMin
// --istio.test.kube.ingressGateway.autoscaleMax
func TestIngressLoadBalancing(t *testing.T) {
	framework.Requires(t, dependency.Mixer, dependency.Kubernetes, dependency.Prometheus, dependency.BookInfo, dependency.Ingress)

	env := framework.AcquireEnvironment(t)

	env.DeployBookInfoOrFail(t)

	env.Configure(t,
		test.JoinConfigs(
			bookinfo.NetworkingBookinfoGateway.LoadOrFail(t),
			bookinfo.NetworkingDestinationRuleAll.LoadOrFail(t),
			bookinfo.NetworkingVirtualServiceAllV1.LoadOrFail(t),
		))

	prometheus := env.GetPrometheusOrFail(t)
	ingress := env.GetIngressOrFail(t)

	rangeStart := time.Now()

	// Send traffic to ingress for the test duration.
	wg := &sync.WaitGroup{}
	wg.Add(numSendTasks + 1)
	go logProgress(testDuration, wg)
	for i := 0; i < numSendTasks; i++ {
		go sendTraffic(testDuration, ingress, wg)
	}
	wg.Wait()

	rangeEnd := time.Now()

	// Gather the CPU usage across all of the ingress gateways.
	query := `sum(rate(container_cpu_usage_seconds_total{pod_name=~'istio-ingressgateway-.*'}[1m])) by (pod_name)`
	v, err := prometheus.API().QueryRange(context.Background(), query, v1.Range{
		Start: rangeStart,
		End:   rangeEnd,
		Step:  step,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Aggregate the per-CPU samples.
	s := getCPUSamples(v, t)

	// Calculate the ratio of the range to the median
	rng := calcRange(s...)
	med := calcMedian(s...)
	ratio := rng / med

	if ratio > threshold {
		t.Fatalf("ratio %f > %f (range=%f, median=%f). CPU samples: %v", ratio, threshold, rng, med, s)
	}
}

func calcRange(sorted ...float64) float64 {
	return sorted[len(sorted)-1] - sorted[0]
}

func calcMedian(sorted ...float64) float64 {
	l := len(sorted)
	if l%2 == 0 {
		return calcMean(sorted[l/2], sorted[l/2+1])
	}
	return sorted[l/2]
}

func calcMean(nums ...float64) float64 {
	total := float64(0)
	for _, n := range nums {
		total += n
	}
	return total / float64(len(nums))
}

// getCPUSamples aggregates the per-ingress CPU values. The provided matrix will have a stream of samples for each ingress.
// We take a sum of these per-ingress values to help simplify the comparison across ingresses. This also helps to smooth
// out any per-measurement noise.
func getCPUSamples(v model.Value, t *testing.T) []float64 {
	if v.Type() != model.ValMatrix {
		t.Fatal(fmt.Errorf("unexpected Prometheus value: %s, expected: %s", v.Type().String(), model.ValMatrix.String()))
	}

	matrix := v.(model.Matrix)
	if matrix.Len() == 0 {
		t.Fatal(fmt.Errorf("received an empty result matrix from Prometheus"))
	}

	totals := make(sort.Float64Slice, matrix.Len())

	// Iterate over the per-Ingress streams.
	for i, stream := range matrix {
		// Sum all of the values for this ingress.
		for _, value := range stream.Values {
			totals[i] += float64(value.Value)
		}
	}

	// Sort the samples in ascending order
	totals.Sort()
	return totals
}

func sendTraffic(duration time.Duration, ingress environment.DeployedIngress, wg *sync.WaitGroup) {
	timeout := time.After(duration)
	for {
		select {
		case <-timeout:
			wg.Done()
			return
		default:
			_, err := ingress.Call("/productpage")
			if err != nil {
				log.Debugf("Send to Ingress failed: %v", err)
			}
		}
	}
}

func logProgress(duration time.Duration, wg *sync.WaitGroup) {
	logTimeRemaining(duration)
	end := time.Now().Add(duration)
	timeout := time.After(duration)
	ticker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-timeout:
			log.Info("Finished sending traffic to ingress")
			ticker.Stop()
			wg.Done()
			return
		case tnow := <-ticker.C:
			timeRemaining := end.Sub(tnow)
			logTimeRemaining(timeRemaining)
		default:
		}
	}
}

func logTimeRemaining(d time.Duration) {
	if d > 0 {
		log.Infof("Sending traffic to ingress. Time remaining: %s ...", formatDuration(d))
	}
}

func formatDuration(d time.Duration) string {
	m := d / time.Minute
	s := (d - (m * time.Minute)) / time.Second
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
