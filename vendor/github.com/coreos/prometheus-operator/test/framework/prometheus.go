// Copyright 2016 The prometheus-operator Authors
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

package framework

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/coreos/prometheus-operator/pkg/prometheus"
	"github.com/pkg/errors"
)

func (f *Framework) MakeBasicPrometheus(ns, name, group string, replicas int32) *monitoringv1.Prometheus {
	return &monitoringv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Annotations: map[string]string{},
		},
		Spec: monitoringv1.PrometheusSpec{
			Replicas: &replicas,
			Version:  prometheus.DefaultPrometheusVersion,
			ServiceMonitorSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"group": group,
				},
			},
			ServiceAccountName: "prometheus",
			RuleSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"role": "rulefile",
				},
			},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceMemory: resource.MustParse("400Mi"),
				},
			},
		},
	}
}

func (f *Framework) AddAlertingToPrometheus(p *monitoringv1.Prometheus, ns, name string) {
	p.Spec.Alerting = &monitoringv1.AlertingSpec{
		Alertmanagers: []monitoringv1.AlertmanagerEndpoints{
			{
				Namespace: ns,
				Name:      fmt.Sprintf("alertmanager-%s", name),
				Port:      intstr.FromString("web"),
			},
		},
	}
}

func (f *Framework) MakeBasicServiceMonitor(name string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"group": name,
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"group": name,
				},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     "web",
					Interval: "30s",
				},
			},
		},
	}
}

func (f *Framework) MakePrometheusService(name, group string, serviceType v1.ServiceType) *v1.Service {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("prometheus-%s", name),
			Labels: map[string]string{
				"group": group,
			},
		},
		Spec: v1.ServiceSpec{
			Type: serviceType,
			Ports: []v1.ServicePort{
				{
					Name:       "web",
					Port:       9090,
					TargetPort: intstr.FromString("web"),
				},
			},
			Selector: map[string]string{
				"prometheus": name,
			},
		},
	}
	return service
}

func (f *Framework) MakeThanosQuerierService(name string) *v1.Service {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http-query",
					Port:       10902,
					TargetPort: intstr.FromString("http"),
				},
			},
			Selector: map[string]string{
				"app": "thanos-query",
			},
		},
	}
	return service
}

func (f *Framework) MakeThanosService(name string) *v1.Service {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "cluster",
					Port:       10900,
					TargetPort: intstr.FromString("cluster"),
				},
			},
			Selector: map[string]string{
				"thanos-peer": "true",
			},
		},
	}
	return service
}

func (f *Framework) CreatePrometheusAndWaitUntilReady(ns string, p *monitoringv1.Prometheus) (*monitoringv1.Prometheus, error) {
	result, err := f.MonClientV1.Prometheuses(ns).Create(p)
	if err != nil {
		return nil, fmt.Errorf("creating %v Prometheus instances failed (%v): %v", p.Spec.Replicas, p.Name, err)
	}

	if err := f.WaitForPrometheusReady(result, 5*time.Minute); err != nil {
		return nil, fmt.Errorf("waiting for %v Prometheus instances timed out (%v): %v", p.Spec.Replicas, p.Name, err)
	}

	return result, nil
}

func (f *Framework) UpdatePrometheusAndWaitUntilReady(ns string, p *monitoringv1.Prometheus) (*monitoringv1.Prometheus, error) {
	result, err := f.MonClientV1.Prometheuses(ns).Update(p)
	if err != nil {
		return nil, err
	}
	if err := f.WaitForPrometheusReady(result, 5*time.Minute); err != nil {
		return nil, fmt.Errorf("failed to update %d Prometheus instances (%v): %v", p.Spec.Replicas, p.Name, err)
	}

	return result, nil
}

func (f *Framework) WaitForPrometheusReady(p *monitoringv1.Prometheus, timeout time.Duration) error {
	var pollErr error

	err := wait.Poll(2*time.Second, timeout, func() (bool, error) {
		st, _, pollErr := prometheus.PrometheusStatus(f.KubeClient, p)

		if pollErr != nil {
			return false, nil
		}

		if st.UpdatedReplicas == *p.Spec.Replicas {
			return true, nil
		}

		return false, nil
	})
	return errors.Wrapf(pollErr, "waiting for Prometheus %v/%v: %v", p.Namespace, p.Name, err)
}

func (f *Framework) DeletePrometheusAndWaitUntilGone(ns, name string) error {
	_, err := f.MonClientV1.Prometheuses(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("requesting Prometheus custom resource %v failed", name))
	}

	if err := f.MonClientV1.Prometheuses(ns).Delete(name, nil); err != nil {
		return errors.Wrap(err, fmt.Sprintf("deleting Prometheus custom resource %v failed", name))
	}

	if err := WaitForPodsReady(
		f.KubeClient,
		ns,
		f.DefaultTimeout,
		0,
		prometheus.ListOptions(name),
	); err != nil {
		return errors.Wrap(
			err,
			fmt.Sprintf("waiting for Prometheus custom resource (%s) to vanish timed out", name),
		)
	}

	return nil
}

func (f *Framework) WaitForPrometheusRunImageAndReady(ns string, p *monitoringv1.Prometheus) error {
	if err := WaitForPodsRunImage(f.KubeClient, ns, int(*p.Spec.Replicas), promImage(p.Spec.Version), prometheus.ListOptions(p.Name)); err != nil {
		return err
	}
	return WaitForPodsReady(
		f.KubeClient,
		ns,
		f.DefaultTimeout,
		int(*p.Spec.Replicas),
		prometheus.ListOptions(p.Name),
	)
}

func promImage(version string) string {
	return fmt.Sprintf("quay.io/prometheus/prometheus:%s", version)
}

func (f *Framework) WaitForTargets(ns, svcName string, amount int) error {
	var targets []*Target

	if err := wait.Poll(time.Second, time.Minute*5, func() (bool, error) {
		var err error
		targets, err = f.GetActiveTargets(ns, svcName)
		if err != nil {
			return false, err
		}

		if len(targets) == amount {
			return true, nil
		}

		return false, nil
	}); err != nil {
		return fmt.Errorf("waiting for targets timed out. %v of %v targets found. %v", len(targets), amount, err)
	}

	return nil
}

func (f *Framework) QueryPrometheusSVC(ns, svcName, endpoint string, query map[string]string) ([]byte, error) {
	ProxyGet := f.KubeClient.CoreV1().Services(ns).ProxyGet
	request := ProxyGet("", svcName, "web", endpoint, query)
	return request.DoRaw()
}

func (f *Framework) GetActiveTargets(ns, svcName string) ([]*Target, error) {
	response, err := f.QueryPrometheusSVC(ns, svcName, "/api/v1/targets", map[string]string{})
	if err != nil {
		return nil, err
	}

	rt := prometheusTargetAPIResponse{}
	if err := json.NewDecoder(bytes.NewBuffer(response)).Decode(&rt); err != nil {
		return nil, err
	}

	return rt.Data.ActiveTargets, nil
}

func (f *Framework) CheckPrometheusFiringAlert(ns, svcName, alertName string) (bool, error) {
	response, err := f.QueryPrometheusSVC(
		ns,
		svcName,
		"/api/v1/query",
		map[string]string{"query": fmt.Sprintf("ALERTS{alertname=\"%v\"}", alertName)},
	)
	if err != nil {
		return false, err
	}

	q := prometheusQueryAPIResponse{}
	if err := json.NewDecoder(bytes.NewBuffer(response)).Decode(&q); err != nil {
		return false, err
	}

	if len(q.Data.Result) != 1 {
		return false, errors.Errorf("expected 1 query result but got %v", len(q.Data.Result))
	}

	alertstate, ok := q.Data.Result[0].Metric["alertstate"]
	if !ok {
		return false, errors.Errorf("could not retrieve 'alertstate' label from query result: %v", q.Data.Result[0])
	}

	return alertstate == "firing", nil
}

func (f *Framework) WaitForPrometheusFiringAlert(ns, svcName, alertName string) error {
	var loopError error

	err := wait.Poll(time.Second, 5*f.DefaultTimeout, func() (bool, error) {
		var firing bool
		firing, loopError = f.CheckPrometheusFiringAlert(ns, svcName, alertName)
		return firing, nil
	})

	if err != nil {
		return errors.Errorf(
			"waiting for alert '%v' to fire: %v: %v",
			alertName,
			err.Error(),
			loopError.Error(),
		)
	}
	return nil
}

type Target struct {
	ScrapeURL string `json:"scrapeUrl"`
}

type targetDiscovery struct {
	ActiveTargets []*Target `json:"activeTargets"`
}

type prometheusTargetAPIResponse struct {
	Status string           `json:"status"`
	Data   *targetDiscovery `json:"data"`
}

type prometheusQueryResult struct {
	Metric map[string]string `json:"metric"`
}

type prometheusQueryData struct {
	Result []prometheusQueryResult `json:"result"`
}

type prometheusQueryAPIResponse struct {
	Status string               `json:"status"`
	Data   *prometheusQueryData `json:"data"`
}
