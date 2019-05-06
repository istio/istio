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

package pilot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/e2e/framework"
)

const (
	churnAppName         = "churn"
	churnAppSelector     = "app=" + churnAppName
	churnAppURL          = "http://" + churnAppName
	churnAppReplicas     = 5
	churnAppMinReady     = 2
	churnTrafficDuration = 1 * time.Minute
	churnTrafficQPS      = 200

	// The number of traffic threads that we should run on each source App pod.
	// We purposely assign multiple threads to the same pod due in order to stress
	// the outbound path for the source pod's Envoy. Envoy's default circuit
	// breaker parameter "max_retries" is set to 3, which means that only 3 retry
	// attempts may be on-going concurrently. If exceeded, the circuit breaker
	// itself will cause 503s. Here we purposely use > 3 sending threads per pod
	// to ensure that these 503s do not occur (i.e. Envoy is properly configured).
	churnTrafficThreadsPerPod = 4
)

// TestPodChurn creates a replicated app and periodically brings down one pod. Traffic is sent during the pod churn
// to ensure that no 503s are received by the application. This verifies that Envoy is properly retrying disconnected
// endpoints.
func TestPodChurn(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/11115")
	// Deploy the churn app
	app := newChurnApp(t)
	defer app.stop()

	// Start a pod churner on the app. The pod churner will periodically bring down one of the pods for the app.
	podChurner := newPodChurner(tc.Kube.Namespace, churnAppSelector, churnAppMinReady, tc.Kube.KubeAccessor)
	defer podChurner.stop()

	// Start a bunch of threads to send traffic to the churn app from the given source apps.
	srcApps := []string{"a", "b"}
	numTgs := len(srcApps) * churnTrafficThreadsPerPod
	tgs := make([]trafficGenerator, 0, numTgs)
	threadQPS := churnTrafficQPS / numTgs
	for _, srcApp := range srcApps {
		for i := 0; i < churnTrafficThreadsPerPod; i++ {
			tg := newTrafficGenerator(t, srcApp, uint16(threadQPS))
			tgs = append(tgs, tg)

			// Start sending traffic.
			tg.start()
			defer tg.close()
		}
	}

	// Merge the results from all traffic generators.
	aggregateResults := make(map[string]int)
	numBadResponses := 0
	totalResponses := 0
	for _, tg := range tgs {
		results, err := tg.awaitResults()
		if err != nil {
			t.Fatal(err)
		}
		for code, count := range results {
			totalResponses += count
			if code != httpOK {
				numBadResponses += count
			}
			aggregateResults[code] += count
		}
	}

	// TODO(https://github.com/istio/istio/issues/9818): Connection_terminated events will not be retried by Envoy.
	//  Until we implement graceful shutdown of Envoy (with lameducking), we will not be able to completely
	//  eliminate 503s in pod churning cases.
	maxBadPercentage := 1.0
	badPercentage := (float64(numBadResponses) / float64(totalResponses)) * 100.0
	if badPercentage > maxBadPercentage {
		t.Fatalf(fmt.Sprintf("received too many bad response codes: %v", aggregateResults))
	} else {
		t.Logf("successfully survived pod churning! Results: %v", aggregateResults)
	}
}

// trafficGenerator generates traffic from a source app to the churn app
type trafficGenerator interface {
	// start generating traffic
	start()

	// awaitResults waits for the traffic generation to end and returns the results or error.
	awaitResults() (map[string]int, error)

	// close any resources
	close()
}

// newTrafficGenerator from the given source app to the churn app.
func newTrafficGenerator(t *testing.T, srcApp string, qps uint16) trafficGenerator {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	return &trafficGeneratorImpl{
		srcApp:    srcApp,
		qps:       qps,
		forwarder: forwardToGrpcPort(t, srcApp),
		wg:        wg,
		results:   make(map[string]int),
	}
}

func forwardToGrpcPort(t *testing.T, app string) kube.PortForwarder {
	// Find the gRPC port for this service.
	svc, err := tc.Kube.KubeAccessor.GetService(tc.Kube.Namespace, app)
	if err != nil {
		t.Fatal(err)
	}
	grpcPort := 0
	for _, port := range svc.Spec.Ports {
		if port.Name == "grpc" {
			grpcPort = int(port.TargetPort.IntVal)
		}
	}
	if grpcPort == 0 {
		t.Fatalf("unable to locate gRPC port for service %s", app)
	}

	// Get the pods for the source App.
	pods := tc.Kube.GetAppPods(primaryCluster)[app]
	if len(pods) == 0 {
		t.Fatalf("missing pod names for app %q from %s cluster", app, primaryCluster)
	}

	pod, err := tc.Kube.KubeAccessor.GetPod(tc.Kube.Namespace, pods[0])
	if err != nil {
		t.Fatalf("failed retrieving pod %s/%s: %v", tc.Kube.Namespace, pods[0], err)
	}
	// Create a port forwarder so that we can send commands app "a" to talk to the churn app.
	forwarder, err := tc.Kube.KubeAccessor.NewPortForwarder(pod, 0, uint16(grpcPort))
	if err != nil {
		t.Fatal(err)
	}

	if err := forwarder.Start(); err != nil {
		t.Fatal(err)
	}
	return forwarder
}

// trafficGeneratorImpl implementation of the trafficGenerator interface.
type trafficGeneratorImpl struct {
	srcApp    string
	qps       uint16
	forwarder kube.PortForwarder
	wg        *sync.WaitGroup

	results map[string]int
	err     error
}

// close implements the trafficGenerator interface.
func (g *trafficGeneratorImpl) close() {
	if g.forwarder != nil {
		_ = g.forwarder.Close()
		g.forwarder = nil
	}
}

// awaitResults implements the trafficGenerator interface.
func (g *trafficGeneratorImpl) awaitResults() (map[string]int, error) {
	g.wg.Wait()

	if g.err != nil {
		return nil, g.err
	}

	return g.results, nil
}

// start implements the trafficGenerator interface.
func (g *trafficGeneratorImpl) start() {
	// Command to send traffic for 1s.
	request := proto.ForwardEchoRequest{
		Url:   churnAppURL,
		Count: int32(g.qps),
		Qps:   int32(g.qps),
	}

	// Create the results for this thread.
	go func() {
		defer g.wg.Done()

		// Create a gRPC client to the source pod.
		c, err := client.New(g.forwarder.Address())
		if err != nil {
			g.err = multierror.Append(g.err, err)
			return
		}
		defer func() {
			_ = c.Close()
		}()

		// Send traffic from the source pod to the churned pods for a period of time.
		timer := time.NewTimer(churnTrafficDuration)

		responseCount := 0
		for {
			select {
			case <-timer.C:
				if responseCount == 0 {
					g.err = fmt.Errorf("service %s unable to communicate to churn app", g.srcApp)
				}
				return
			default:
				responses, err := c.ForwardEcho(context.Background(), &request)
				if err != nil {
					// Retry on RPC errors.
					log.Infof("retrying failed control RPC to app: %s. Error: %v", g.srcApp, err)
					continue
				}
				responseCount += len(responses)
				for _, resp := range responses {
					g.results[resp.Code]++
				}
			}
		}
	}()
}

// churnApp is a multi-replica app, the pods for which will be periodically deleted.
type churnApp struct {
	app *framework.App
}

// newChurnApp creates and deploys the churnApp
func newChurnApp(t *testing.T) *churnApp {
	a := getApp(churnAppName,
		churnAppName,
		churnAppReplicas,
		8080,
		80,
		9090,
		90,
		7070,
		70,
		"v1",
		true,
		false,
		true,
		true)
	app := &churnApp{
		app: &a,
	}
	if err := tc.Kube.AppManager.DeployApp(app.app); err != nil {
		t.Fatal(err)
		return nil
	}

	fetchFn := tc.Kube.KubeAccessor.NewPodFetch(tc.Kube.Namespace, churnAppSelector)
	if _, err := tc.Kube.KubeAccessor.WaitUntilPodsAreReady(fetchFn); err != nil {
		app.stop()
		t.Fatal(err)
		return nil
	}
	return app
}

// stop the churn app (i.e. undeploy)
func (a *churnApp) stop() {
	if a.app != nil {
		_ = tc.Kube.AppManager.UndeployApp(a.app)
		a.app = nil
	}
}

// podChurner periodically deletes pods in the churn app.
type podChurner struct {
	accessor      *kube.Accessor
	namespace     string
	labelSelector string
	minReplicas   int
	stopCh        chan struct{}
}

// newPodChurner creates and starts the podChurner.
func newPodChurner(namespace, labelSelector string, minReplicas int, accessor *kube.Accessor) *podChurner {
	churner := &podChurner{
		accessor:      accessor,
		namespace:     namespace,
		labelSelector: labelSelector,
		minReplicas:   minReplicas,
		stopCh:        make(chan struct{}),
	}

	// Start churning.
	go func() {
		// Periodically check to see if we need to kill a pod.
		ticker := time.NewTicker(500 * time.Millisecond)

		for {
			select {
			case <-churner.stopCh:
				ticker.Stop()
				return
			case <-ticker.C:
				readyPods := churner.getReadyPods()
				if len(readyPods) <= churner.minReplicas {
					// Wait until we have at least minReplicas before taking one down.
					continue
				}

				// Delete the oldest ready pod.
				_ = churner.accessor.DeletePod(churner.namespace, readyPods[0])
			}
		}
	}()

	return churner
}

// stop the pod churner
func (c *podChurner) stop() {
	c.stopCh <- struct{}{}
}

func (c *podChurner) getReadyPods() []string {
	pods, err := c.accessor.GetPods(c.namespace, c.labelSelector)
	if err != nil {
		log.Warnf("failed getting pods for ns=%s, label selector=%s, %v", c.namespace, c.labelSelector, err)
		return make([]string, 0)
	}

	// Sort the pods oldest first.
	sort.Slice(pods, func(i, j int) bool {
		p1 := pods[i]
		p2 := pods[j]

		t1 := &p1.CreationTimestamp
		t2 := &p2.CreationTimestamp

		if t1.Before(t2) {
			return true
		}
		if t2.Before(t1) {
			return false
		}

		// Use name as the tie-breaker
		return strings.Compare(p1.Name, p2.Name) < 0
	})

	// Return the names of all pods that are ready.
	out := make([]string, 0, len(pods))
	for _, p := range pods {
		if e := kube.CheckPodReady(&p); e == nil {
			out = append(out, p.Name)
		}
	}
	return out
}
