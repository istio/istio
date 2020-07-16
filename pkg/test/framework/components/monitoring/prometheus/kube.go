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

package prometheus

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	prometheusApi "github.com/prometheus/client_golang/api"
	prometheusApiV1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/monitoring"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceName = "prometheus"
	appName     = "prometheus"
)

var (
	retryTimeout = retry.Timeout(time.Second * 120)
	retryDelay   = retry.Delay(time.Second * 5)

	_ Instance = &kubeComponent{}
	// _ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id resource.ID

	api       prometheusApiV1.API
	forwarder istioKube.PortForwarder
	cluster   resource.Cluster
	cleanup   func() error
}

func getPrometheusYaml() (string, error) {
	yamlBytes, err := ioutil.ReadFile(filepath.Join(env.IstioSrc, "samples/addons/prometheus.yaml"))
	if err != nil {
		return "", err
	}
	yaml := string(yamlBytes)
	// For faster tests, drop scrape interval
	yaml = strings.ReplaceAll(yaml, "scrape_interval: 15s", "scrape_interval: 5s")
	yaml = strings.ReplaceAll(yaml, "scrape_timeout: 10s", "scrape_timeout: 5s")
	return yaml, nil
}

func installPrometheus(ctx resource.Context, ns string) error {
	yaml, err := getPrometheusYaml()
	if err != nil {
		return err
	}
	return ctx.Config().ApplyYAML(ns, yaml)
}

func removePrometheus(ctx resource.Context, ns string) error {
	yaml, err := getPrometheusYaml()
	if err != nil {
		return err
	}
	return ctx.Config().DeleteYAML(ns, yaml)
}

func newKube(ctx resource.Context, cfgIn Config) (Instance, error) {
	c := &kubeComponent{
		cluster: resource.ClusterOrDefault(cfgIn.Cluster, ctx.Environment()),
	}
	c.id = ctx.TrackResource(c)
	// Find the Prometheus pod and service, and start forwarding a local port.
	cfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	if !cfgIn.SkipDeploy {
		if err := installPrometheus(ctx, cfg.TelemetryNamespace); err != nil {
			return nil, err
		}

		c.cleanup = func() error {
			return removePrometheus(ctx, cfg.TelemetryNamespace)
		}
	}
	fetchFn := testKube.NewSinglePodFetch(c.cluster, cfg.TelemetryNamespace, fmt.Sprintf("app=%s", appName))
	pods, err := testKube.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	svc, err := c.cluster.CoreV1().Services(cfg.TelemetryNamespace).Get(context.TODO(), serviceName, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, err
	}
	port := uint16(svc.Spec.Ports[0].Port)

	forwarder, err := c.cluster.NewPortForwarder(pod.Name, pod.Namespace, "", 0, int(port))
	if err != nil {
		return nil, err
	}

	if err := forwarder.Start(); err != nil {
		return nil, err
	}
	c.forwarder = forwarder
	scopes.Framework.Debugf("initialized Prometheus port forwarder: %v", forwarder.Address())

	address := fmt.Sprintf("http://%s", forwarder.Address())
	var client prometheusApi.Client
	client, err = prometheusApi.NewClient(prometheusApi.Config{Address: address})
	if err != nil {
		return nil, err
	}

	c.api = prometheusApiV1.NewAPI(client)

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) Requests(ctx context.Context, dims monitoring.TrafficDimensions) (float64, error) {
	query := buildQuery(dims)

	//fmt.Println("QUERY: ", query)
	val, warnings, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failure issuing requests query %q: %v", query, err)
	}

	for _, warning := range warnings {
		fmt.Println("warning: ", warning)
	}

	if val.Type() != model.ValVector {
		return 0, fmt.Errorf("value not a model.Vector; was %s", val.Type().String())
	}

	value := val.(model.Vector)
	valueCount := 0.0
	for _, sample := range value {
		valueCount += float64(sample.Value)
	}

	return valueCount, nil
}

func buildQuery(dims monitoring.TrafficDimensions) string {
	var sumClause strings.Builder
	var queryBody strings.Builder
	trafficCtx := dims.Context
	v := reflect.ValueOf(trafficCtx)
	addDimensionsForValue(v, &sumClause, &queryBody)
	v = reflect.ValueOf(dims.Request)
	addDimensionsForValue(v, &sumClause, &queryBody)
	v = reflect.ValueOf(dims.Destination)
	addDimensionsWithPrefix(v, "destination_", &sumClause, &queryBody)
	v = reflect.ValueOf(dims.Source)
	addDimensionsWithPrefix(v, "source_", &sumClause, &queryBody)

	sc := strings.TrimSuffix(sumClause.String(), ",")
	qb := strings.TrimSuffix(queryBody.String(), ",")

	return fmt.Sprintf("sum by (%s) (istio_requests_total{%s})", sc, qb)
}

func addDimensionsForValue(v reflect.Value, sumClause, queryBody *strings.Builder) {
	addDimensionsWithPrefix(v, "", sumClause, queryBody)
}

func addDimensionsWithPrefix(v reflect.Value, prefix string, sumClause, queryBody *strings.Builder) {
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
		addDimension(tag, prefix, strVal, sumClause, queryBody)
	}
}

func addDimension(tag, prefix, dimension string, sumClause, queryBody *strings.Builder) {
	var label string
	args := strings.Split(tag, ",")
	for _, arg := range args {
		if strings.HasPrefix(arg, "prom=") {
			label = strings.TrimPrefix(arg, "prom=")
			break
		}
	}
	if label == "" {
		return
	}
	sumClause.WriteString(prefix + label + ",")
	queryBody.WriteString(fmt.Sprintf("%s%s=~\"%s\",", prefix, label, dimension))
}
