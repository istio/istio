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

package stackdriver

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	jsonpb "github.com/golang/protobuf/jsonpb"
	cloudtracepb "google.golang.org/genproto/googleapis/devtools/cloudtrace/v1"
	ltype "google.golang.org/genproto/googleapis/logging/type"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
	kubeApiCore "k8s.io/api/core/v1"

	istioKube "istio.io/istio/pkg/kube"
	environ "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/namespace"
	edgespb "istio.io/istio/pkg/test/framework/components/stackdriver/edges"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

const (
	stackdriverNamespace = "istio-stackdriver"
	stackdriverPort      = 8091
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id        resource.ID
	ns        namespace.Instance
	forwarder istioKube.PortForwarder
	cluster   resource.Cluster
	address   string
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		cluster: ctx.Clusters().GetOrDefault(cfg.Cluster),
	}
	c.id = ctx.TrackResource(c)
	var err error
	scopes.Framework.Info("=== BEGIN: Deploy Stackdriver ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("stackdriver deployment failed: %v", err) // nolint:golint
			scopes.Framework.Infof("=== FAILED: Deploy Stackdriver ===")
			_ = c.Close()
		} else {
			scopes.Framework.Info("=== SUCCEEDED: Deploy Stackdriver ===")
		}
	}()

	c.ns, err = namespace.New(ctx, namespace.Config{
		Prefix: stackdriverNamespace,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create %s Namespace for Stackdriver install; err:%v", stackdriverNamespace, err)
	}

	// apply stackdriver YAML
	if err := c.cluster.ApplyYAMLFiles(c.ns.Name(), environ.StackdriverInstallFilePath); err != nil {
		return nil, fmt.Errorf("failed to apply rendered %s, err: %v", environ.StackdriverInstallFilePath, err)
	}

	fetchFn := testKube.NewSinglePodFetch(c.cluster, c.ns.Name(), "app=stackdriver")
	pods, err := testKube.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	forwarder, err := c.cluster.NewPortForwarder(pod.Name, pod.Namespace, "", 0, stackdriverPort)
	if err != nil {
		return nil, err
	}

	if err := forwarder.Start(); err != nil {
		return nil, err
	}
	c.forwarder = forwarder
	scopes.Framework.Debugf("initialized stackdriver port forwarder: %v", forwarder.Address())

	var svc *kubeApiCore.Service
	if svc, _, err = testKube.WaitUntilServiceEndpointsAreReady(c.cluster, c.ns.Name(), "stackdriver"); err != nil {
		scopes.Framework.Infof("Error waiting for Stackdriver service to be available: %v", err)
		return nil, err
	}

	c.address = fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].TargetPort.IntVal)
	scopes.Framework.Infof("Stackdriver in-cluster address: %s", c.address)

	return c, nil
}

func (c *kubeComponent) ListTimeSeries() ([]*monitoringpb.TimeSeries, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("http://" + c.forwarder.Address() + "/timeseries")
	if err != nil {
		return []*monitoringpb.TimeSeries{}, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []*monitoringpb.TimeSeries{}, err
	}
	var r monitoringpb.ListTimeSeriesResponse
	err = jsonpb.UnmarshalString(string(body), &r)
	if err != nil {
		return []*monitoringpb.TimeSeries{}, err
	}
	var ret []*monitoringpb.TimeSeries
	for _, t := range r.TimeSeries {
		// Remove fields that do not need verification
		t.Points = nil
		delete(t.Resource.Labels, "cluster_name")
		delete(t.Resource.Labels, "location")
		delete(t.Resource.Labels, "project_id")
		delete(t.Resource.Labels, "pod_name")
		ret = append(ret, t)
		t.Metadata = nil
	}
	return ret, nil
}

func (c *kubeComponent) ListLogEntries() ([]*loggingpb.LogEntry, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("http://" + c.forwarder.Address() + "/logentries")
	if err != nil {
		return []*loggingpb.LogEntry{}, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []*loggingpb.LogEntry{}, err
	}
	var r loggingpb.ListLogEntriesResponse
	err = jsonpb.UnmarshalString(string(body), &r)
	if err != nil {
		return []*loggingpb.LogEntry{}, err
	}
	var ret []*loggingpb.LogEntry
	for _, l := range r.Entries {
		// Remove fields that do not need verification
		l.Timestamp = nil
		l.Severity = ltype.LogSeverity_DEFAULT
		if l.HttpRequest != nil {
			l.HttpRequest.ResponseSize = 0
			l.HttpRequest.RequestSize = 0
			l.HttpRequest.ServerIp = ""
			l.HttpRequest.RemoteIp = ""
			l.HttpRequest.Latency = nil
		}
		delete(l.Labels, "request_id")
		delete(l.Labels, "source_name")
		delete(l.Labels, "destination_ip")
		delete(l.Labels, "destination_name")
		delete(l.Labels, "connection_id")
		delete(l.Labels, "upstream_host")
		delete(l.Labels, "connection_state")
		delete(l.Labels, "source_ip")
		delete(l.Labels, "source_port")
		delete(l.Labels, "total_sent_bytes")
		delete(l.Labels, "total_received_bytes")
		ret = append(ret, l)
	}
	return ret, nil
}

func (c *kubeComponent) ListTrafficAssertions() ([]*edgespb.TrafficAssertion, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("http://" + c.forwarder.Address() + "/trafficassertions")
	if err != nil {
		return []*edgespb.TrafficAssertion{}, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []*edgespb.TrafficAssertion{}, err
	}
	var rta edgespb.ReportTrafficAssertionsRequest
	err = jsonpb.UnmarshalString(string(body), &rta)
	if err != nil {
		return []*edgespb.TrafficAssertion{}, err
	}

	return rta.TrafficAssertions, nil
}

func (c *kubeComponent) ListTraces() ([]*cloudtracepb.Trace, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("http://" + c.forwarder.Address() + "/traces")
	if err != nil {
		return []*cloudtracepb.Trace{}, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []*cloudtracepb.Trace{}, err
	}
	var traceResp cloudtracepb.ListTracesResponse
	err = jsonpb.UnmarshalString(string(body), &traceResp)
	if err != nil {
		return []*cloudtracepb.Trace{}, err
	}

	return traceResp.Traces, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	return nil
}

func (c *kubeComponent) GetStackdriverNamespace() string {
	return c.ns.Name()
}

func (c *kubeComponent) Address() string {
	return c.address
}
