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

package status

import (
	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/slices"
)

const (
	StatusTrue  = "True"
	StatusFalse = "False"
)

func GetConditionFromSpec(cfg config.Config, condition string) *v1alpha1.IstioCondition {
	c, ok := cfg.Status.(*v1alpha1.IstioStatus)
	if !ok {
		return nil
	}
	return GetCondition(c.Conditions, condition)
}

func GetBoolConditionFromSpec(cfg config.Config, condition string, defaultValue bool) bool {
	c, ok := cfg.Status.(*v1alpha1.IstioStatus)
	if !ok {
		return defaultValue
	}
	return GetBoolCondition(c.Conditions, condition, defaultValue)
}

func GetBoolCondition(conditions []*v1alpha1.IstioCondition, condition string, defaultValue bool) bool {
	got := GetCondition(conditions, condition)
	if got == nil {
		return defaultValue
	}
	if got.Status == StatusTrue {
		return true
	}
	if got.Status == StatusFalse {
		return false
	}
	return defaultValue
}

func GetCondition(conditions []*v1alpha1.IstioCondition, condition string) *v1alpha1.IstioCondition {
	for _, cond := range conditions {
		if cond.Type == condition {
			return cond
		}
	}
	return nil
}

func UpdateIstioConfigCondition(cfg config.Config, condition *v1alpha1.IstioCondition) config.Config {
	cfg = cfg.DeepCopy()
	var status *v1alpha1.IstioStatus
	if cfg.Status == nil {
		cfg.Status = &v1alpha1.IstioStatus{}
	}
	status = cfg.Status.(*v1alpha1.IstioStatus)
	status.Conditions = updateCondition(status.Conditions, condition)
	return cfg
}

func updateCondition(conditions []*v1alpha1.IstioCondition, condition *v1alpha1.IstioCondition) []*v1alpha1.IstioCondition {
	for i, cond := range conditions {
		if cond.Type == condition.Type {
			conditions[i] = condition
			return conditions
		}
	}

	return append(conditions, condition)
}

func DeleteIstioConfigCondition(cfg config.Config, condition string) config.Config {
	c, ok := cfg.Status.(*v1alpha1.IstioStatus)
	if !ok {
		return cfg
	}
	if GetCondition(c.Conditions, condition) == nil {
		return cfg
	}
	cfg = cfg.DeepCopy()
	status := cfg.Status.(*v1alpha1.IstioStatus)
	status.Conditions = deleteCondition(status.Conditions, condition)
	return cfg
}

func deleteCondition(conditions []*v1alpha1.IstioCondition, condition string) []*v1alpha1.IstioCondition {
	conditions = slices.FilterInPlace(conditions, func(c *v1alpha1.IstioCondition) bool {
		return c.Type != condition
	})

	return conditions
}
