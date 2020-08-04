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
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	prometheusApi "github.com/prometheus/client_golang/api"
	prometheusApiV1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	serviceName = "prometheus"
	appName     = "prometheus"
)

var (
	retryTimeout = retry.Timeout(time.Second * 120)
	retryDelay   = retry.Delay(time.Second * 5)

	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
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
		cluster: ctx.Clusters().GetOrDefault(cfgIn.Cluster),
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

// API implements environment.DeployedPrometheus.
func (c *kubeComponent) API() prometheusApiV1.API {
	return c.api
}

func (c *kubeComponent) WaitForQuiesce(format string, args ...interface{}) (model.Value, error) {
	var previous model.Value

	time.Sleep(time.Second * 1)

	value, err := retry.Do(func() (interface{}, bool, error) {

		query, err := tmpl.Evaluate(fmt.Sprintf(format, args...), map[string]string{})
		if err != nil {
			return nil, true, err
		}

		scopes.Framework.Debugf("WaitForQuiesce running: %q", query)

		v, _, err := c.api.Query(context.Background(), query, time.Now())
		if err != nil {
			return nil, false, fmt.Errorf("error querying Prometheus: %v", err)
		}
		scopes.Framework.Debugf("WaitForQuiesce received: %v", v)

		if previous == nil {
			previous = v
			return nil, false, nil
		}

		if !areEqual(v, previous) {
			scopes.Framework.Debugf("WaitForQuiesce: \n%v\n!=\n%v\n", v, previous)
			previous = v
			return nil, false, fmt.Errorf("unable to quiesce for query: %q", query)
		}

		return v, true, nil
	}, retryTimeout, retryDelay)

	var v model.Value
	if value != nil {
		v = value.(model.Value)
	}
	return v, err
}

func (c *kubeComponent) WaitForQuiesceOrFail(t test.Failer, format string, args ...interface{}) model.Value {
	v, err := c.WaitForQuiesce(format, args...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (c *kubeComponent) WaitForOneOrMore(format string, args ...interface{}) (model.Value, error) {

	value, err := retry.Do(func() (interface{}, bool, error) {
		query, err := tmpl.Evaluate(fmt.Sprintf(format, args...), map[string]string{})
		if err != nil {
			return nil, true, err
		}

		scopes.Framework.Debugf("WaitForOneOrMore running: %q", query)

		v, _, err := c.api.Query(context.Background(), query, time.Now())
		if err != nil {
			return nil, false, fmt.Errorf("error querying Prometheus: %v", err)
		}
		scopes.Framework.Debugf("WaitForOneOrMore received: %v", v)

		switch v.Type() {
		case model.ValScalar, model.ValString:
			return v, true, nil

		case model.ValVector:
			value := v.(model.Vector)
			value = reduce(value, map[string]string{})
			if len(value) == 0 {
				return nil, false, fmt.Errorf("value not found (query: %q)", query)
			}
			return v, true, nil

		default:
			return nil, true, fmt.Errorf("unhandled value type: %v", v.Type())
		}
	}, retryTimeout, retryDelay)

	var v model.Value
	if value != nil {
		v = value.(model.Value)
	}
	return v, err
}

func (c *kubeComponent) WaitForOneOrMoreOrFail(t test.Failer, format string, args ...interface{}) model.Value {
	val, err := c.WaitForOneOrMore(format, args...)
	if err != nil {
		t.Fatal(err)
	}
	return val
}

func reduce(v model.Vector, labels map[string]string) model.Vector {
	if labels == nil {
		return v
	}

	reduced := make([]*model.Sample, 0)

	for _, s := range v {
		nameCount := len(labels)
		for k, v := range s.Metric {
			if labelVal, ok := labels[string(k)]; ok && labelVal == string(v) {
				nameCount--
			}
		}
		if nameCount == 0 {
			reduced = append(reduced, s)
		}
	}

	return reduced
}

func (c *kubeComponent) Sum(val model.Value, labels map[string]string) (float64, error) {
	if val.Type() != model.ValVector {
		return 0, fmt.Errorf("value not a model.Vector; was %s", val.Type().String())
	}

	value := val.(model.Vector)
	value = reduce(value, labels)

	valueCount := 0.0
	for _, sample := range value {
		valueCount += float64(sample.Value)
	}

	if valueCount > 0.0 {
		return valueCount, nil
	}
	return 0, fmt.Errorf("value not found for %#v", labels)
}

func (c *kubeComponent) SumOrFail(t test.Failer, val model.Value, labels map[string]string) float64 {
	v, err := c.Sum(val, labels)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	c.forwarder.Close()
	if c.cleanup != nil {
		return c.cleanup()
	}
	return nil
}

// check equality without considering timestamps
func areEqual(v1, v2 model.Value) bool {
	if v1.Type() != v2.Type() {
		return false
	}

	switch v1.Type() {
	case model.ValNone:
		return true
	case model.ValString:
		vs1 := v1.(*model.String)
		vs2 := v2.(*model.String)
		return vs1.Value == vs2.Value
	case model.ValScalar:
		ss1 := v1.(*model.Scalar)
		ss2 := v2.(*model.Scalar)
		return ss1.Value == ss2.Value
	case model.ValVector:
		vec1 := v1.(model.Vector)
		vec2 := v2.(model.Vector)
		if len(vec1) != len(vec2) {
			scopes.Framework.Debugf("Prometheus.areEqual vector value size mismatch %d != %d", len(vec1), len(vec2))
			return false
		}

		for i := 0; i < len(vec1); i++ {
			if !vec1[i].Metric.Equal(vec2[i].Metric) {
				scopes.Framework.Debugf(
					"Prometheus.areEqual vector metric mismatch (at:%d): \n%v\n != \n%v\n",
					i, vec1[i].Metric, vec2[i].Metric)
				return false
			}

			if vec1[i].Value != vec2[i].Value {
				scopes.Framework.Debugf(
					"Prometheus.areEqual vector value mismatch (at:%d): %f != %f",
					i, vec1[i].Value, vec2[i].Value)
				return false
			}
		}
		return true

	default:
		panic("unrecognized type " + v1.Type().String())
	}
}
