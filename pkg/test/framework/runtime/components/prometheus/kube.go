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

package prometheus

import (
	"context"
	"fmt"
	"io"
	"time"

	prometheusApi "github.com/prometheus/client_golang/api"
	prometheusApiV1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	testContext "istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
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
	retryDelay   = retry.Delay(time.Second * 20)

	_ components.Prometheus = &kubeComponent{}
	_ api.Component         = &kubeComponent{}
	_ io.Closer             = &kubeComponent{}
)

type kubeComponent struct {
	scope     lifecycle.Scope
	api       prometheusApiV1.API
	forwarder testKube.PortForwarder
	env       *kube.Environment
}

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.Prometheus
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

func (c *kubeComponent) Start(ctx testContext.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}
	c.env = env

	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()

	// Find the Prometheus pod and service, and start forwarding a local port.
	n := env.NamespaceForScope(c.scope)

	fetchFn := env.Accessor.NewSinglePodFetch(n, fmt.Sprintf("app.kubernetes.io/name=%s", appName))
	if err := env.Accessor.WaitUntilPodsAreReady(fetchFn); err != nil {
		return err
	}
	pods, err := fetchFn()
	if err != nil {
		return err
	}
	pod := pods[0]

	svc, err := env.Accessor.GetService(n, serviceName)
	if err != nil {
		return err
	}
	port := uint16(svc.Spec.Ports[0].Port)

	options := &testKube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}
	forwarder, err := env.Accessor.NewPortForwarder(options, 0, port)
	if err != nil {
		return err
	}

	if err := forwarder.Start(); err != nil {
		return err
	}
	c.forwarder = forwarder
	scopes.Framework.Debugf("initialized Prometheus port forwarder: %v", forwarder.Address())

	address := fmt.Sprintf("http://%s", forwarder.Address())
	var client prometheusApi.Client
	client, err = prometheusApi.NewClient(prometheusApi.Config{Address: address})
	if err != nil {
		return err
	}

	c.api = prometheusApiV1.NewAPI(client)
	return nil
}

// API implements environment.DeployedPrometheus.
func (c *kubeComponent) API() prometheusApiV1.API {
	return c.api
}

func (c *kubeComponent) WaitForQuiesce(format string, args ...interface{}) (model.Value, error) {
	var previous model.Value

	time.Sleep(time.Second * 5)

	value, err := retry.Do(func() (interface{}, bool, error) {

		query, err := c.env.Evaluate(fmt.Sprintf(format, args...))
		if err != nil {
			return nil, true, err
		}

		scopes.Framework.Debugf("WaitForQuiesce running: %q", query)

		v, err := c.api.Query(context.Background(), query, time.Now())
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

func (c *kubeComponent) WaitForOneOrMore(format string, args ...interface{}) error {

	time.Sleep(time.Second * 5)

	_, err := retry.Do(func() (interface{}, bool, error) {
		query, err := c.env.Evaluate(fmt.Sprintf(format, args...))
		if err != nil {
			return nil, true, err
		}

		scopes.Framework.Debugf("WaitForOneOrMore running: %q", query)

		v, err := c.api.Query(context.Background(), query, time.Now())
		if err != nil {
			return nil, false, fmt.Errorf("error querying Prometheus: %v", err)
		}
		scopes.Framework.Debugf("WaitForOneOrMore received: %v", v)

		switch v.Type() {
		case model.ValScalar, model.ValString:
			return nil, true, nil

		case model.ValVector:
			value := v.(model.Vector)
			value = reduce(value, map[string]string{})
			if len(value) == 0 {
				return nil, false, fmt.Errorf("value not found (query: %q)", query)
			}
			return nil, true, nil

		default:
			return nil, true, fmt.Errorf("unhandled value type: %v", v.Type())
		}
	}, retryTimeout, retryDelay)

	return err
}

func reduce(v model.Vector, labels map[string]string) model.Vector {
	if labels == nil {
		return v
	}

	reduced := []*model.Sample{}

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

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	return c.forwarder.Close()
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
					"Prometheus.areEqual vector metric mismatch (at:%d): %d != %d",
					i, vec1[i].Metric, vec2[i].Metric)
				return false
			}

			if vec1[i].Value != vec2[i].Value {
				scopes.Framework.Debugf(
					"Prometheus.areEqual vector  value mismatch (at:%d): \n%v\n!=\n%v\n",
					i, vec1[i].Metric, vec2[i].Metric)
				return false
			}
		}
		return true

	default:
		panic("unrecognized type " + v1.Type().String())
	}
}
