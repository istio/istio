// Copyright 2018 The Operator-SDK Authors
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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// RunningReason - Condition is running
	RunningReason = "Running"
	// SuccessfulReason - Condition is running due to reconcile being successful
	SuccessfulReason = "Successful"
	// FailedReason - Condition is failed due to ansible failure
	FailedReason = "Failed"
	// UnknownFailedReason - Condition is unknown
	UnknownFailedReason = "Unknown"
)

const (
	// RunningMessage - message for running reason.
	RunningMessage = "Running reconciliation"
	// SuccessfulMessage - message for successful reason.
	SuccessfulMessage = "Awaiting next reconciliation"
)

// NewCondition -  condition
func NewCondition(condType ConditionType, status v1.ConditionStatus, ansibleResult *AnsibleResult, reason, message string) *Condition {
	return &Condition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
		AnsibleResult:      ansibleResult,
	}
}

// GetCondition returns the condition with the provided type.
func GetCondition(status Status, condType ConditionType) *Condition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetCondition updates the scheduledReport to include the provided condition. If the condition that
// we are about to add already exists and has the same status and reason then we are not going to update.
func SetCondition(status *Status, condition Condition) {
	currentCond := GetCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	// Do not update lastTransitionTime if the status of the condition doesn't change.
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
}

// RemoveCondition removes the scheduledReport condition with the provided type.
func RemoveCondition(status *Status, condType ConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

// filterOutCondition returns a new slice of scheduledReport conditions without conditions with the provided type.
func filterOutCondition(conditions []Condition, condType ConditionType) []Condition {
	var newConditions []Condition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
