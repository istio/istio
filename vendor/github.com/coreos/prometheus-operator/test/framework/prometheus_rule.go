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
	"fmt"
	"time"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (f *Framework) MakeBasicRule(ns, name string, groups []monitoringv1.RuleGroup) *monitoringv1.PrometheusRule {
	return &monitoringv1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"role": "rulefile",
			},
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: groups,
		},
	}
}

func (f *Framework) CreateRule(ns string, ar *monitoringv1.PrometheusRule) (*monitoringv1.PrometheusRule, error) {
	result, err := f.MonClientV1.PrometheusRules(ns).Create(ar)
	if err != nil {
		return nil, fmt.Errorf("creating %v RuleFile failed: %v", ar.Name, err)
	}

	return result, nil
}

func (f *Framework) MakeAndCreateFiringRule(ns, name, alertName string) (*monitoringv1.PrometheusRule, error) {
	groups := []monitoringv1.RuleGroup{
		{
			Name: alertName,
			Rules: []monitoringv1.Rule{
				{
					Alert: alertName,
					Expr:  intstr.FromString("vector(1)"),
				},
			},
		},
	}
	file := f.MakeBasicRule(ns, name, groups)

	result, err := f.CreateRule(ns, file)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// WaitForRule waits for a rule file with a given name to exist in a given
// namespace.
func (f *Framework) WaitForRule(ns, name string) error {
	return wait.Poll(time.Second, f.DefaultTimeout, func() (bool, error) {
		_, err := f.MonClientV1.PrometheusRules(ns).Get(name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, nil
	})
}

func (f *Framework) UpdateRule(ns string, ar *monitoringv1.PrometheusRule) (*monitoringv1.PrometheusRule, error) {
	result, err := f.MonClientV1.PrometheusRules(ns).Update(ar)
	if err != nil {
		return nil, fmt.Errorf("updating %v RuleFile failed: %v", ar.Name, err)
	}

	return result, nil
}

func (f *Framework) DeleteRule(ns string, r string) error {
	err := f.MonClientV1.PrometheusRules(ns).Delete(r, &metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleteing %v Prometheus rule in namespace %v failed: %v", r, ns, err.Error())
	}

	return nil
}
