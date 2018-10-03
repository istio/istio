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
	"reflect"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"istio.io/istio/pkg/test/util"

	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/kube"
)

var (

	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &component{}

	_ io.Closer                      = &deployedPrometheus{}
	_ environment.DeployedPrometheus = &deployedPrometheus{}
)

type component struct {
}

type deployedPrometheus struct {
	api       v1.API
	forwarder kube.PortForwarder
	env       *kubernetes.Implementation
}

// ID implements implements component.Component.
func (c *component) ID() dependency.Instance {
	return dependency.Prometheus
}

// Requires implements implements component.Component.
func (c *component) Requires() []dependency.Instance {
	return []dependency.Instance{}
}

// Init implements implements component.Component.
func (c *component) Init(ctx environment.ComponentContext, _ map[dependency.Instance]interface{}) (interface{}, error) {
	env, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %v", reflect.TypeOf(ctx.Environment()))
	}

	// Find the Prometheus pod and service, and start forwarding a local port.
	pod, err := env.Accessor.WaitForPodBySelectors(env.KubeSettings().IstioSystemNamespace, "app=prometheus")
	if err != nil {
		return nil, err
	}

	svc, err := env.Accessor.GetService(env.KubeSettings().IstioSystemNamespace, "prometheus")
	if err != nil {
		return nil, err
	}
	port := uint16(svc.Spec.Ports[0].Port)

	options := &kube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}
	forwarder, err := kube.NewPortForwarder(env.KubeSettings().KubeConfig, options, 0, port)
	if err != nil {
		return nil, err
	}

	if err := forwarder.Start(); err != nil {
		return nil, err
	}
	scopes.Framework.Debugf("initialized Prometheus port forwarder: %v", forwarder.Address())

	client, err := api.NewClient(api.Config{Address: fmt.Sprintf("http://%s", forwarder.Address())})
	if err != nil {
		return nil, err
	}
	a := v1.NewAPI(client)

	return &deployedPrometheus{
		api:       a,
		forwarder: forwarder,
		env:       env,
	}, nil
}

// API implements environment.DeployedPrometheus.
func (b *deployedPrometheus) API() v1.API {
	return b.api
}

func (b *deployedPrometheus) WaitForQuiesce(format string, args ...interface{}) (model.Value, error) {
	var previous model.Value

	time.Sleep(time.Second * 5)

	value, err := util.Retry(time.Second*30, time.Second*5, func() (interface{}, bool, error) {

		query, err := b.env.Evaluate(fmt.Sprintf(format, args...))
		if err != nil {
			return nil, true, err
		}

		scopes.Framework.Debugf("WaitForQuiesce running: %q", query)

		v, err := b.api.Query(context.Background(), query, time.Now())
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
	})

	var v model.Value
	if value != nil {
		v = value.(model.Value)
	}
	return v, err
}

func (b *deployedPrometheus) WaitForOneOrMore(format string, args ...interface{}) error {

	time.Sleep(time.Second * 5)

	_, err := util.Retry(time.Second*30, time.Second*5, func() (interface{}, bool, error) {

		query, err := b.env.Evaluate(fmt.Sprintf(format, args...))
		if err != nil {
			return nil, true, err
		}

		scopes.Framework.Debugf("WaitForOneOrMore running: %q", query)

		v, err := b.api.Query(context.Background(), query, time.Now())
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

	})

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

func (b *deployedPrometheus) Sum(val model.Value, labels map[string]string) (float64, error) {
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
func (b *deployedPrometheus) Close() error {
	return b.forwarder.Close()
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
