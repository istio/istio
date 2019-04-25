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
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/coreos/prometheus-operator/pkg/alertmanager"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/pkg/errors"
)

var ValidAlertmanagerConfig = `global:
  resolve_timeout: 5m
route:
  group_by: ['job']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'webhook'
receivers:
- name: 'webhook'
  webhook_configs:
  - url: 'http://alertmanagerwh:30500/'
`

func (f *Framework) MakeBasicAlertmanager(name string, replicas int32) *monitoringv1.Alertmanager {
	return &monitoringv1.Alertmanager{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: monitoringv1.AlertmanagerSpec{
			Replicas: &replicas,
			LogLevel: "debug",
		},
	}
}

func (f *Framework) MakeAlertmanagerService(name, group string, serviceType v1.ServiceType) *v1.Service {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("alertmanager-%s", name),
			Labels: map[string]string{
				"group": group,
			},
		},
		Spec: v1.ServiceSpec{
			Type: serviceType,
			Ports: []v1.ServicePort{
				{
					Name:       "web",
					Port:       9093,
					TargetPort: intstr.FromString("web"),
				},
			},
			Selector: map[string]string{
				"alertmanager": name,
			},
		},
	}

	return service
}

func (f *Framework) SecretFromYaml(filepath string) (*v1.Secret, error) {
	manifest, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}

	s := v1.Secret{}
	err = yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&s)
	if err != nil {
		return nil, err
	}

	return &s, nil
}

func (f *Framework) AlertmanagerConfigSecret(ns, name string) (*v1.Secret, error) {
	s, err := f.SecretFromYaml("../../test/framework/ressources/alertmanager-main-secret.yaml")
	if err != nil {
		return nil, err
	}

	s.Name = name
	s.Namespace = ns
	return s, nil
}

func (f *Framework) CreateAlertmanagerAndWaitUntilReady(ns string, a *monitoringv1.Alertmanager) (*monitoringv1.Alertmanager, error) {
	amConfigSecretName := fmt.Sprintf("alertmanager-%s", a.Name)
	s, err := f.AlertmanagerConfigSecret(ns, amConfigSecretName)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("making alertmanager config secret %v failed", amConfigSecretName))
	}
	_, err = f.KubeClient.CoreV1().Secrets(ns).Create(s)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("creating alertmanager config secret %v failed", s.Name))
	}

	a, err = f.MonClientV1.Alertmanagers(ns).Create(a)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("creating alertmanager %v failed", a.Name))
	}

	return a, f.WaitForAlertmanagerReady(ns, a.Name, int(*a.Spec.Replicas))
}

func (f *Framework) WaitForAlertmanagerReady(ns, name string, replicas int) error {
	err := WaitForPodsReady(
		f.KubeClient,
		ns,
		5*time.Minute,
		replicas,
		alertmanager.ListOptions(name),
	)

	return errors.Wrap(err, fmt.Sprintf("failed to create an Alertmanager cluster (%s) with %d instances", name, replicas))
}

func (f *Framework) UpdateAlertmanagerAndWaitUntilReady(ns string, a *monitoringv1.Alertmanager) (*monitoringv1.Alertmanager, error) {
	a, err := f.MonClientV1.Alertmanagers(ns).Update(a)
	if err != nil {
		return nil, err
	}

	err = WaitForPodsReady(
		f.KubeClient,
		ns,
		5*time.Minute,
		int(*a.Spec.Replicas),
		alertmanager.ListOptions(a.Name),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update %d Alertmanager instances (%s): %v", a.Spec.Replicas, a.Name, err)
	}

	return a, nil
}

func (f *Framework) DeleteAlertmanagerAndWaitUntilGone(ns, name string) error {
	_, err := f.MonClientV1.Alertmanagers(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("requesting Alertmanager tpr %v failed", name))
	}

	if err := f.MonClientV1.Alertmanagers(ns).Delete(name, nil); err != nil {
		return errors.Wrap(err, fmt.Sprintf("deleting Alertmanager tpr %v failed", name))
	}

	if err := WaitForPodsReady(
		f.KubeClient,
		ns,
		f.DefaultTimeout,
		0,
		alertmanager.ListOptions(name),
	); err != nil {
		return errors.Wrap(err, fmt.Sprintf("waiting for Alertmanager tpr (%s) to vanish timed out", name))
	}

	return f.KubeClient.CoreV1().Secrets(ns).Delete(fmt.Sprintf("alertmanager-%s", name), nil)
}

func amImage(version string) string {
	return fmt.Sprintf("quay.io/prometheus/alertmanager:%s", version)
}

func (f *Framework) WaitForAlertmanagerInitializedMesh(ns, name string, amountPeers int) error {
	var pollError error
	err := wait.Poll(time.Second, time.Minute*5, func() (bool, error) {
		amStatus, err := f.GetAlertmanagerStatus(ns, name)
		if err != nil {
			return false, err
		}

		// Starting from AM v0.15.0 'MeshStatus' is called 'ClusterStatus'.
		// Therefor we need to check for both.
		if amStatus.Data.MeshStatus == nil && amStatus.Data.ClusterStatus == nil {
			pollError = fmt.Errorf("do not have a cluster / mesh status")
			return false, nil
		}

		if amStatus.Data.getAmountPeers() == amountPeers {
			return true, nil
		}

		var addresses []string
		// Starting from AM v0.15.0 'MeshStatus' is called 'ClusterStatus'. This
		// is abstracted via `getPeers()`.
		for _, p := range amStatus.Data.getPeers() {
			addresses = append(addresses, p.Address)
		}

		pollError = fmt.Errorf(
			"failed to get correct amount of peers, expected %d, got %d, addresses %v",
			amountPeers,
			amStatus.Data.getAmountPeers(),
			strings.Join(addresses, ","),
		)

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("failed to wait for initialized alertmanager mesh: %v: %v", err, pollError)
	}

	return nil
}

func (f *Framework) GetAlertmanagerStatus(ns, n string) (amAPIStatusResp, error) {
	var amStatus amAPIStatusResp
	request := ProxyGetPod(f.KubeClient, ns, n, "/api/v1/status")
	resp, err := request.DoRaw()
	if err != nil {
		return amStatus, err
	}

	if err := json.Unmarshal(resp, &amStatus); err != nil {
		return amStatus, err
	}

	return amStatus, nil
}

func (f *Framework) CreateSilence(ns, n string) (string, error) {
	var createSilenceResponse amAPICreateSilResp

	request := ProxyPostPod(
		f.KubeClient, ns, n,
		"/api/v1/silences",
		`{"id":"","createdBy":"Max Mustermann","comment":"1234","startsAt":"2030-04-09T09:16:15.114Z","endsAt":"2031-04-09T11:16:15.114Z","matchers":[{"name":"test","value":"123","isRegex":false}]}`,
	)
	resp, err := request.DoRaw()
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(resp, &createSilenceResponse); err != nil {
		return "", err
	}

	if createSilenceResponse.Status != "success" {
		return "", errors.Errorf(
			"expected Alertmanager to return 'success', but got '%v' instead",
			createSilenceResponse.Status,
		)
	}

	return createSilenceResponse.Data.SilenceID, nil
}

// alert represents an alert that can be posted to the /api/v1/alerts endpoint
// of an Alertmanager.
// Taken from github.com/prometheus/common/model/alert.go.Alert.
type alert struct {
	// Label value pairs for purpose of aggregation, matching, and disposition
	// dispatching. This must minimally include an "alertname" label.
	Labels map[string]string `json:"labels"`

	// Extra key/value information which does not define alert identity.
	Annotations map[string]string `json:"annotations"`

	// The known time range for this alert. Both ends are optional.
	StartsAt     time.Time `json:"startsAt,omitempty"`
	EndsAt       time.Time `json:"endsAt,omitempty"`
	GeneratorURL string    `json:"generatorURL"`
}

// SendAlertToAlertmanager sends an alert to the alertmanager in the given
// namespace (ns) with the given name (n).
func (f *Framework) SendAlertToAlertmanager(ns, n string, start time.Time) error {
	alerts := []*alert{&alert{
		Labels: map[string]string{
			"alertname": "ExampleAlert", "prometheus": "my-prometheus",
		},
		Annotations:  map[string]string{},
		StartsAt:     start,
		GeneratorURL: "http://prometheus-test-0:9090/graph?g0.expr=vector%281%29\u0026g0.tab=1",
	}}
	b, err := json.Marshal(alerts)
	if err != nil {
		return err
	}

	var postAlertResp amAPIPostAlertResp
	request := ProxyPostPod(f.KubeClient, ns, n, "api/v1/alerts", string(b))
	resp, err := request.DoRaw()
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp, &postAlertResp); err != nil {
		return err
	}

	if postAlertResp.Status != "success" {
		return errors.Errorf("expected Alertmanager to return 'success' but got %q instead", postAlertResp.Status)
	}

	return nil
}

func (f *Framework) GetSilences(ns, n string) ([]amAPISil, error) {
	var getSilencesResponse amAPIGetSilResp

	request := ProxyGetPod(f.KubeClient, ns, n, "/api/v1/silences")
	resp, err := request.DoRaw()
	if err != nil {
		return getSilencesResponse.Data, err
	}

	if err := json.Unmarshal(resp, &getSilencesResponse); err != nil {
		return getSilencesResponse.Data, err
	}

	if getSilencesResponse.Status != "success" {
		return getSilencesResponse.Data, errors.Errorf(
			"expected Alertmanager to return 'success', but got '%v' instead",
			getSilencesResponse.Status,
		)
	}

	return getSilencesResponse.Data, nil
}

// WaitForAlertmanagerConfigToContainString retrieves the Alertmanager
// configuration via the Alertmanager's API and checks if it contains the given
// string.
func (f *Framework) WaitForAlertmanagerConfigToContainString(ns, amName, expectedString string) error {
	err := wait.Poll(10*time.Second, time.Minute*5, func() (bool, error) {
		config, err := f.GetAlertmanagerStatus(ns, "alertmanager-"+amName+"-0")
		if err != nil {
			return false, err
		}

		if strings.Contains(config.Data.ConfigYAML, expectedString) {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("failed to wait for alertmanager config to contain %q: %v", expectedString, err)
	}

	return nil
}

type amAPICreateSilResp struct {
	Status string             `json:"status"`
	Data   amAPICreateSilData `json:"data"`
}

type amAPIPostAlertResp struct {
	Status string `json:"status"`
}

type amAPICreateSilData struct {
	SilenceID string `json:"silenceId"`
}

type amAPIGetSilResp struct {
	Status string     `json:"status"`
	Data   []amAPISil `json:"data"`
}

type amAPISil struct {
	ID        string `json:"id"`
	CreatedBy string `json:"createdBy"`
}

type amAPIStatusResp struct {
	Data amAPIStatusData `json:"data"`
}

type amAPIStatusData struct {
	ClusterStatus *clusterStatus `json:"clusterStatus,omitempty"`
	MeshStatus    *clusterStatus `json:"meshStatus,omitempty"`
	ConfigYAML    string         `json:"configYAML"`
}

// Starting from AM v0.15.0 'MeshStatus' is called 'ClusterStatus'
func (s *amAPIStatusData) getPeers() []peer {
	if s.MeshStatus != nil {
		return s.MeshStatus.Peers
	}
	return s.ClusterStatus.Peers
}

func (s *amAPIStatusData) getAmountPeers() int {
	return len(s.getPeers())
}

type peer struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type clusterStatus struct {
	Peers []peer `json:"peers"`
}
