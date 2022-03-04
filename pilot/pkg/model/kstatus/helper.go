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

package kstatus

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config"
)

const (
	StatusTrue  = "True"
	StatusFalse = "False"
)

// InvertStatus returns the opposite of the provided status. If an invalid status is passed in, False is returned
func InvertStatus(status metav1.ConditionStatus) metav1.ConditionStatus {
	switch status {
	case StatusFalse:
		return StatusTrue
	default:
		return StatusFalse
	}
}

// WrappedStatus provides a wrapper around a status message that keeps track of whether or not any
// changes have been made. This allows users to declarative write status, without worrying about
// tracking changes. When read to commit (typically to Kubernetes), any messages with Dirty=false can
// be discarded.
type WrappedStatus struct {
	// Status is the object that is wrapped.
	config.Status
	// Dirty indicates if this object has been modified at all.
	// Note: only changes wrapped in Mutate are tracked.
	Dirty bool
}

func Wrap(s config.Status) *WrappedStatus {
	return &WrappedStatus{config.DeepCopy(s), false}
}

func (w *WrappedStatus) Mutate(f func(s config.Status) config.Status) {
	if w.Status == nil {
		return
	}
	old := config.DeepCopy(w.Status)
	w.Status = f(w.Status)
	// TODO: change this to be more efficient. Likely we allow modifications via WrappedStatus that
	// modify specific things (ie conditions).
	if !reflect.DeepEqual(old, w.Status) {
		w.Dirty = true
	}
}

func (w *WrappedStatus) Unwrap() config.Status {
	return w.Status
}

var EmptyCondition = metav1.Condition{}

func GetCondition(conditions []metav1.Condition, condition string) metav1.Condition {
	for _, cond := range conditions {
		if cond.Type == condition {
			return cond
		}
	}
	return EmptyCondition
}

// UpdateConditionIfChanged updates a condition if it has been changed.
func UpdateConditionIfChanged(conditions []metav1.Condition, condition metav1.Condition) []metav1.Condition {
	ret := append([]metav1.Condition(nil), conditions...)
	idx := -1
	for i, cond := range ret {
		if cond.Type == condition.Type {
			idx = i
			break
		}
	}

	if idx == -1 {
		ret = append(ret, condition)
		return ret
	}
	if ret[idx].Message == condition.Message &&
		ret[idx].ObservedGeneration == condition.ObservedGeneration &&
		ret[idx].Status == condition.Status {
		// Skip update, no changes
		return conditions
	}
	ret[idx] = condition

	return ret
}

// CreateCondition sets a condition only if it has not already been set
func CreateCondition(conditions []metav1.Condition, condition metav1.Condition, unsetReason string) []metav1.Condition {
	ret := append([]metav1.Condition(nil), conditions...)
	idx := -1
	for i, cond := range ret {
		if cond.Type == condition.Type {
			idx = i
			if cond.Reason == unsetReason {
				// Condition is set, but its for unsetReason. This is needed because some conditions have defaults
				ret[idx] = condition
				return ret
			}
			break
		}
	}

	if idx == -1 {
		// Not found! We should set it
		ret = append(ret, condition)
	}
	return ret
}
