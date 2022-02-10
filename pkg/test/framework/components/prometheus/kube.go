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

package prometheus // import "istio.io/istio/pkg/test/framework/components/prometheus"

import (
	"context"
	"fmt"
	"io"
	"os"
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
	"istio.io/istio/pkg/test/framework/components/cluster"
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
	retryDelay   = retry.Delay(time.Second * 1)

	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type kubeComponent struct {
	id resource.ID

	api       map[string]prometheusApiV1.API
	forwarder map[string]istioKube.PortForwarder
	clusters  cluster.Clusters
}

func getPrometheusYaml() (string, error) {
	yamlBytes, err := os.ReadFile(filepath.Join(env.IstioSrc, "samples/addons/prometheus.yaml"))
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
	if err := ctx.ConfigKube().ApplyYAMLNoCleanup(ns, yaml); err != nil {
		return err
	}
	ctx.Cleanup(func() {
		_ = ctx.ConfigKube().DeleteYAML(ns, yaml)
	})
	return nil
}

func newKube(ctx resource.Context, cfgIn Config) (Instance, error) {
	c := &kubeComponent{
		clusters: ctx.Clusters(),
	}
	c.id = ctx.TrackResource(c)
	c.api = make(map[string]prometheusApiV1.API)
	c.forwarder = make(map[string]istioKube.PortForwarder)
	cfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	if !cfgIn.SkipDeploy {
		if err := installPrometheus(ctx, cfg.TelemetryNamespace); err != nil {
			return nil, err
		}
	}
	for _, cls := range ctx.Clusters().Kube() {
		scopes.Framework.Debugf("Installing Prometheus on cluster: %s", cls.Name())
		// Find the Prometheus pod and service, and start forwarding a local port.
		fetchFn := testKube.NewSinglePodFetch(cls, cfg.TelemetryNamespace, fmt.Sprintf("app=%s", appName))
		pods, err := testKube.WaitUntilPodsAreReady(fetchFn)
		if err != nil {
			return nil, err
		}
		pod := pods[0]

		svc, err := cls.CoreV1().Services(cfg.TelemetryNamespace).Get(context.TODO(), serviceName, kubeApiMeta.GetOptions{})
		if err != nil {
			return nil, err
		}
		port := uint16(svc.Spec.Ports[0].Port)

		forwarder, err := cls.NewPortForwarder(pod.Name, pod.Namespace, "", 0, int(port))
		if err != nil {
			return nil, err
		}

		if err := forwarder.Start(); err != nil {
			return nil, err
		}
		c.forwarder[cls.Name()] = forwarder
		scopes.Framework.Debugf("initialized Prometheus port forwarder: %v", forwarder.Address())

		address := fmt.Sprintf("http://%s", forwarder.Address())
		var client prometheusApi.Client
		client, err = prometheusApi.NewClient(prometheusApi.Config{Address: address})
		if err != nil {
			return nil, err
		}

		c.api[cls.Name()] = prometheusApiV1.NewAPI(client)
	}
	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// API implements environment.DeployedPrometheus.
func (c *kubeComponent) API() prometheusApiV1.API {
	return c.api[c.clusters.Default().Name()]
}

func (c *kubeComponent) APIForCluster(cluster cluster.Cluster) prometheusApiV1.API {
	return c.api[cluster.Name()]
}

func (c *kubeComponent) Query(cluster cluster.Cluster, format string) (model.Value, error) {
	value, err := retry.UntilComplete(func() (interface{}, bool, error) {
		var err error
		query, err := tmpl.Evaluate(format, map[string]string{})
		if err != nil {
			return nil, true, err
		}

		scopes.Framework.Debugf("Query running: %q", query)

		v, _, err := c.api[cluster.Name()].Query(context.Background(), query, time.Now())
		if err != nil {
			return nil, false, fmt.Errorf("error querying Prometheus: %v", err)
		}
		scopes.Framework.Debugf("Query received: %v", v)

		switch v.Type() {
		case model.ValScalar, model.ValString:
			return v, true, nil

		case model.ValVector:
			value := v.(model.Vector)
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

func (c *kubeComponent) QueryOrFail(t test.Failer, cluster cluster.Cluster, format string) model.Value {
	val, err := c.Query(cluster, format)
	if err != nil {
		t.Fatal(err)
	}
	return val
}

func (c *kubeComponent) QuerySum(cluster cluster.Cluster, query string) (float64, error) {
	val, err := c.Query(cluster, query)
	if err != nil {
		return 0, err
	}
	got, err := Sum(val)
	if err != nil {
		return 0, fmt.Errorf("could not find metric value: %v", err)
	}
	return got, nil
}

func (c *kubeComponent) QuerySumOrFail(t test.Failer, cluster cluster.Cluster, query string) float64 {
	v, err := c.QuerySum(cluster, query)
	if err != nil {
		t.Fatal("failed QuerySum: %v", err)
	}
	return v
}

func Sum(val model.Value) (float64, error) {
	if val.Type() != model.ValVector {
		return 0, fmt.Errorf("value not a model.Vector; was %s", val.Type().String())
	}

	value := val.(model.Vector)

	valueCount := 0.0
	for _, sample := range value {
		valueCount += float64(sample.Value)
	}

	if valueCount > 0.0 {
		return valueCount, nil
	}
	return 0, fmt.Errorf("value not found")
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	for _, forwarder := range c.forwarder {
		forwarder.Close()
	}
	return nil
}
