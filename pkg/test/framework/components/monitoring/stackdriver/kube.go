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
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"time"

	istioKube "istio.io/istio/pkg/kube"
	environ "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/monitoring"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"

	jsonpb "github.com/golang/protobuf/jsonpb"
	monitoredres "google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
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
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{
		cluster: resource.ClusterOrDefault(cfg.Cluster, ctx.Environment()),
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
		ret = append(ret, t)
	}
	return ret, nil
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

func monitoredResourceTemplate(dims monitoring.TrafficDimensions) *monitoredres.MonitoredResource {
	mr := &monitoredres.MonitoredResource{Labels: make(map[string]string)}

	// we'd have to figure out some way of determining `gce_instance`
	if dims.Context.Reporter == "source" {
		mr.Type = "k8s_pod"

		if dims.Context.SourceCluster != "" {
			mr.Labels["cluster_name"] = dims.Context.SourceCluster
		}
		if dims.Source.Location != "" {
			mr.Labels["location"] = dims.Source.Location
		}
		if dims.Source.Namespace != "" {
			mr.Labels["namespace_name"] = dims.Source.Namespace
		}
		if dims.Source.InstanceName != "" {
			mr.Labels["pod_name"] = dims.Source.InstanceName
		}

		return mr
	}

	mr.Type = "k8s_container"
	if dims.Context.DestinationCluster != "" {
		mr.Labels["cluster_name"] = dims.Context.DestinationCluster
	}
	if dims.Destination.Location != "" {
		mr.Labels["location"] = dims.Destination.Location
	}
	if dims.Destination.Namespace != "" {
		mr.Labels["namespace_name"] = dims.Destination.Namespace
	}
	if dims.Destination.InstanceName != "" {
		mr.Labels["pod_name"] = dims.Destination.InstanceName
	}
	if dims.Destination.Container != "" {
		mr.Labels["container_name"] = dims.Destination.Container
	}
	return mr
}

func metricLabels(dims monitoring.TrafficDimensions) map[string]string {
	l := make(map[string]string)

	v := reflect.ValueOf(dims.Context)
	for k, v := range labelsForValue(v) {
		l[k] = v
	}
	v = reflect.ValueOf(dims.Request)
	for k, v := range labelsForValue(v) {
		l[k] = v
	}
	v = reflect.ValueOf(dims.Destination)
	for k, v := range labelsForValueWithPrefix(v, "destination_") {
		l[k] = v
	}
	v = reflect.ValueOf(dims.Source)
	for k, v := range labelsForValueWithPrefix(v, "source_") {
		l[k] = v
	}

	return l
}

func (c *kubeComponent) Requests(ctx context.Context, dims monitoring.TrafficDimensions) (float64, error) {

	wantMetricType := "istio.io/service/server/request_count"
	if dims.Context.Reporter == "source" {
		wantMetricType = "istio.io/service/client/request_count"
	}

	wantMR := monitoredResourceTemplate(dims)
	wantLabels := metricLabels(dims)

	timeseries, err := c.ListTimeSeries()
	if err != nil {
		return 0, fmt.Errorf("failure retrieving time-series: %v", err)
	}
	var sum int64
	for _, ts := range timeseries {
		if ts.Metric.Type != wantMetricType {
			continue
		}
		res := ts.Resource
		if res == nil {
			continue
		}
		if res.Type != wantMR.Type {
			continue
		}
		found := true
		for k, v := range wantMR.Labels {
			if res.Labels[k] != v {
				found = false
				break
			}
		}
		if !found {
			continue
		}
		for k, v := range wantLabels {
			if ts.Metric.Labels[k] != v {
				found = false
				break
			}
		}
		if !found {
			continue
		}
		for _, point := range ts.Points {
			val := point.Value
			v := val.GetInt64Value()
			sum += v
		}
	}
	return float64(sum), nil
}

func labelsForValue(v reflect.Value) map[string]string {
	return labelsForValueWithPrefix(v, "")
}

func labelsForValueWithPrefix(v reflect.Value, prefix string) map[string]string {
	l := make(map[string]string)
	for i := 0; i < v.NumField(); i++ {
		tag := v.Type().Field(i).Tag.Get(monitoring.TagName)
		// Skip if tag is not defined or ignored
		if tag == "" || tag == "-" {
			continue
		}

		strVal, ok := v.Field(i).Interface().(string)
		if !ok || strVal == "" {
			continue
		}

		label := label(tag, prefix)
		if label == "" {
			fmt.Printf("empty label for %s", v.Type().Field(i).Name)
			continue
		}
		l[label] = strVal
	}
	return l
}

func label(tag, prefix string) string {
	var label string
	args := strings.Split(tag, ",")
	for _, arg := range args {
		if strings.HasPrefix(arg, "stackdriver=") {
			label = strings.TrimPrefix(arg, "stackdriver=")
			break
		}
	}
	return prefix + label
}
