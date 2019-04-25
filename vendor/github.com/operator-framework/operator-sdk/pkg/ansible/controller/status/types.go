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
	"encoding/json"
	"time"

	"github.com/operator-framework/operator-sdk/pkg/ansible/runner/eventapi"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("controller.status")

const (
	host = "localhost"
)

// AnsibleResult - encapsulation of the ansible result.
type AnsibleResult struct {
	Ok               int                `json:"ok"`
	Changed          int                `json:"changed"`
	Skipped          int                `json:"skipped"`
	Failures         int                `json:"failures"`
	TimeOfCompletion eventapi.EventTime `json:"completion"`
}

// NewAnsibleResultFromStatusJobEvent - creates a Ansible status from job event.
func NewAnsibleResultFromStatusJobEvent(je eventapi.StatusJobEvent) *AnsibleResult {
	// ok events.
	a := &AnsibleResult{TimeOfCompletion: je.Created}
	if v, ok := je.EventData.Changed[host]; ok {
		a.Changed = v
	}
	if v, ok := je.EventData.Ok[host]; ok {
		a.Ok = v
	}
	if v, ok := je.EventData.Skipped[host]; ok {
		a.Skipped = v
	}
	if v, ok := je.EventData.Failures[host]; ok {
		a.Failures = v
	}
	return a
}

// NewAnsibleResultFromMap - creates a Ansible status from a job event.
func NewAnsibleResultFromMap(sm map[string]interface{}) *AnsibleResult {
	//Create Old top level status
	// ok events.
	a := &AnsibleResult{}
	if v, ok := sm["changed"]; ok {
		a.Changed = int(v.(int64))
	}
	if v, ok := sm["ok"]; ok {
		a.Ok = int(v.(int64))
	}
	if v, ok := sm["skipped"]; ok {
		a.Skipped = int(v.(int64))
	}
	if v, ok := sm["failures"]; ok {
		a.Failures = int(v.(int64))
	}
	if v, ok := sm["completion"]; ok {
		s := v.(string)
		if err := a.TimeOfCompletion.UnmarshalJSON([]byte(s)); err != nil {
			log.Error(err, "Failed to unmarshal time of completion for ansible result")
		}
	}
	return a
}

// ConditionType - type of condition
type ConditionType string

const (
	// RunningConditionType - condition type of running.
	RunningConditionType ConditionType = "Running"
	// FailureConditionType - condition type of failure.
	FailureConditionType ConditionType = "Failure"
)

// Condition - the condition for the ansible operator.
type Condition struct {
	Type               ConditionType      `json:"type"`
	Status             v1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time        `json:"lastTransitionTime"`
	AnsibleResult      *AnsibleResult     `json:"ansibleResult,omitempty"`
	Reason             string             `json:"reason"`
	Message            string             `json:"message"`
}

func createConditionFromMap(cm map[string]interface{}) Condition {
	ct, ok := cm["type"].(string)
	if !ok {
		//If we do not find the string we are defaulting
		// to make sure we can at least update the status.
		ct = string(RunningConditionType)
	}
	status, ok := cm["status"].(string)
	if !ok {
		status = string(v1.ConditionTrue)
	}
	reason, ok := cm["reason"].(string)
	if !ok {
		reason = RunningReason
	}
	message, ok := cm["message"].(string)
	if !ok {
		message = RunningMessage
	}
	asm, ok := cm["ansibleResult"].(map[string]interface{})
	var ansibleResult *AnsibleResult
	if ok {
		ansibleResult = NewAnsibleResultFromMap(asm)
	}
	ltts, ok := cm["lastTransitionTime"].(string)
	ltt := metav1.Now()
	if ok {
		t, err := time.Parse("2006-01-02T15:04:05Z", ltts)
		if err != nil {
			log.Info("Unable to parse time for status condition", "Time", ltts)
		} else {
			ltt = metav1.NewTime(t)
		}
	}
	return Condition{
		Type:               ConditionType(ct),
		Status:             v1.ConditionStatus(status),
		LastTransitionTime: ltt,
		Reason:             reason,
		Message:            message,
		AnsibleResult:      ansibleResult,
	}
}

// Status - The status for custom resources managed by the operator-sdk.
type Status struct {
	Conditions   []Condition            `json:"conditions"`
	CustomStatus map[string]interface{} `json:"-"`
}

// CreateFromMap - create a status from the map
func CreateFromMap(statusMap map[string]interface{}) Status {
	customStatus := make(map[string]interface{})
	for key, value := range statusMap {
		if key != "conditions" {
			customStatus[key] = value
		}
	}
	conditionsInterface, ok := statusMap["conditions"].([]interface{})
	if !ok {
		return Status{Conditions: []Condition{}, CustomStatus: customStatus}
	}
	conditions := []Condition{}
	for _, ci := range conditionsInterface {
		cm, ok := ci.(map[string]interface{})
		if !ok {
			log.Info("Unknown condition, removing condition", "ConditionInterface", ci)
			continue
		}
		conditions = append(conditions, createConditionFromMap(cm))
	}
	return Status{Conditions: conditions, CustomStatus: customStatus}
}

// GetJSONMap - gets the map value for the status object.
// This is used to set the status on the CR.
// This is needed because the unstructured type has special rules around DeepCopy.
// If you do not convert the status to the map, then DeepCopy for the
// unstructured will fail and throw runtime exceptions.
// Please note that this will return an empty map on error.
func (status *Status) GetJSONMap() map[string]interface{} {
	b, err := json.Marshal(status)
	if err != nil {
		log.Error(err, "Unable to marshal json")
		return status.CustomStatus
	}
	if err := json.Unmarshal(b, &status.CustomStatus); err != nil {
		log.Error(err, "Unable to unmarshal json")
	}
	return status.CustomStatus
}
