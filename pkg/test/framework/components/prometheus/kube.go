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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	prometheusApi "github.com/prometheus/client_golang/api"
	prometheusApiV1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

const (
	serviceName = "prometheus"
	appName     = "prometheus"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

type clusterConnInfo struct {
	cls     cluster.Cluster
	podName string
	podNS   string
	port    int
}

type kubeComponent struct {
	id       resource.ID
	mu       sync.RWMutex
	stopOnce sync.Once
	stop     chan struct{}

	api       map[string]prometheusApiV1.API
	forwarder map[string]istioKube.PortForwarder
	connInfo  map[string]*clusterConnInfo
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
	yaml = strings.ReplaceAll(yaml, "namespace: istio-system", fmt.Sprintf("namespace: %s", ns))
	if err := ctx.ConfigKube().YAML(ns, yaml).Apply(apply.NoCleanup); err != nil {
		return err
	}
	ctx.CleanupConditionally(func() {
		_ = ctx.ConfigKube().YAML(ns, yaml).Delete()
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
	c.connInfo = make(map[string]*clusterConnInfo)
	c.stop = make(chan struct{})
	cfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	if !cfgIn.SkipDeploy {
		if err := installPrometheus(ctx, cfg.TelemetryNamespace); err != nil {
			return nil, err
		}
	}
	for _, cls := range ctx.Clusters() {
		scopes.Framework.Debugf("Installing Prometheus on cluster: %s", cls.Name())
		// Find the Prometheus pod and service, and start forwarding a local port.
		fetchFn := testKube.NewSinglePodFetch(cls, cfg.TelemetryNamespace, fmt.Sprintf("app.kubernetes.io/name=%s", appName))
		pods, err := testKube.WaitUntilPodsAreReady(fetchFn)
		if err != nil {
			return nil, err
		}
		pod := pods[0]

		svc, err := cls.Kube().CoreV1().Services(cfg.TelemetryNamespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		port := int(svc.Spec.Ports[0].Port)

		forwarder, err := cls.NewPortForwarder(pod.Name, pod.Namespace, "", 0, port)
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
		c.connInfo[cls.Name()] = &clusterConnInfo{
			cls:     cls,
			podName: pod.Name,
			podNS:   pod.Namespace,
			port:    port,
		}
		go c.watchForwarder(cls.Name())
	}
	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// API implements environment.DeployedPrometheus.
func (c *kubeComponent) API() prometheusApiV1.API {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.api[c.clusters.Default().Name()]
}

func (c *kubeComponent) APIForCluster(cluster cluster.Cluster) prometheusApiV1.API {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.api[cluster.Name()]
}

func (c *kubeComponent) isConnected(clsName string) bool {
	fwd := c.forwarder[clsName]
	if fwd == nil {
		return false
	}
	select {
	case <-fwd.ErrChan():
		return false
	default:
		return true
	}
}

func (c *kubeComponent) reconnect(clsName string) error {
	info := c.connInfo[clsName]
	if fwd := c.forwarder[clsName]; fwd != nil {
		fwd.Close()
		c.forwarder[clsName] = nil
	}
	fwd, err := info.cls.NewPortForwarder(info.podName, info.podNS, "", 0, info.port)
	if err != nil {
		return err
	}
	if err := fwd.Start(); err != nil {
		fwd.Close()
		return err
	}
	c.forwarder[clsName] = fwd

	address := fmt.Sprintf("http://%s", fwd.Address())
	client, err := prometheusApi.NewClient(prometheusApi.Config{Address: address})
	if err != nil {
		fwd.Close()
		c.forwarder[clsName] = nil
		return err
	}
	c.api[clsName] = prometheusApiV1.NewAPI(client)
	return nil
}

func (c *kubeComponent) watchForwarder(clsName string) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.mu.Lock()
			if !c.isConnected(clsName) {
				scopes.Framework.Warnf("prometheus port forward for cluster %s terminated, reconnecting", clsName)
				if err := c.reconnect(clsName); err != nil {
					scopes.Framework.Warnf("prometheus port forward reconnect for cluster %s failed: %v", clsName, err)
				} else {
					scopes.Framework.Warnf("prometheus port forward for cluster %s reconnected", clsName)
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *kubeComponent) RawQuery(cluster cluster.Cluster, promQL string) (model.Value, error) {
	scopes.Framework.Debugf("Query running: %s", promQL)

	c.mu.RLock()
	api := c.api[cluster.Name()]
	c.mu.RUnlock()

	v, _, err := api.Query(context.Background(), promQL, time.Now())
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus: %v", err)
	}
	scopes.Framework.Debugf("Query received: %v", v)

	switch v.Type() {
	case model.ValScalar, model.ValString:
		return v, nil

	case model.ValVector:
		value := v.(model.Vector)
		if len(value) == 0 {
			return nil, fmt.Errorf("value not found (query: %v)", promQL)
		}
		return v, nil

	default:
		return nil, fmt.Errorf("unhandled value type: %v", v.Type())
	}
}

func (c *kubeComponent) Query(cluster cluster.Cluster, query Query) (model.Value, error) {
	return c.RawQuery(cluster, query.String())
}

func (c *kubeComponent) QuerySum(cluster cluster.Cluster, query Query) (float64, error) {
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
	c.stopOnce.Do(func() { close(c.stop) })
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, forwarder := range c.forwarder {
		if forwarder != nil {
			forwarder.Close()
		}
	}
	return nil
}

type Query struct {
	Metric      string
	Aggregation string
	Labels      map[string]string
}

func (q Query) String() string {
	query := q.Metric + `{`

	keys := []string{}
	for k := range q.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := q.Labels[k]
		query += fmt.Sprintf(`%s=%q,`, k, v)
	}
	query += "}"
	if q.Aggregation != "" {
		query = fmt.Sprintf(`%s(%s)`, q.Aggregation, query)
	}
	return query
}
