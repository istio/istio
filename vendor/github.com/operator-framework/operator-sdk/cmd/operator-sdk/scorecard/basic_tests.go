// Copyright 2019 The Operator-SDK Authors
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

package scorecard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

// Run - implements Test interface
func (t *CheckSpecTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t, MaximumPoints: 1}
	err := t.Client.Get(ctx, types.NamespacedName{Namespace: t.CR.GetNamespace(), Name: t.CR.GetName()}, t.CR)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("error getting custom resource: %v", err))
		return res
	}
	if t.CR.Object["spec"] != nil {
		res.EarnedPoints++
	}
	if res.EarnedPoints != 1 {
		res.Suggestions = append(res.Suggestions, "Add a 'spec' field to your Custom Resource")
	}
	return res
}

// Run - implements Test interface
func (t *CheckStatusTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t, MaximumPoints: 1}
	err := t.Client.Get(ctx, types.NamespacedName{Namespace: t.CR.GetNamespace(), Name: t.CR.GetName()}, t.CR)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("error getting custom resource: %v", err))
		return res
	}
	if t.CR.Object["status"] != nil {
		res.EarnedPoints++
	}
	if res.EarnedPoints != 1 {
		res.Suggestions = append(res.Suggestions, "Add a 'status' field to your Custom Resource")
	}
	return res
}

// Run - implements Test interface
func (t *WritingIntoCRsHasEffectTest) Run(ctx context.Context) *TestResult {
	res := &TestResult{Test: t, MaximumPoints: 1}
	logs, err := getProxyLogs(t.ProxyPod)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("error getting proxy logs: %v", err))
		return res
	}
	msgMap := make(map[string]interface{})
	for _, msg := range strings.Split(logs, "\n") {
		if err := json.Unmarshal([]byte(msg), &msgMap); err != nil {
			continue
		}
		method, ok := msgMap["method"].(string)
		if !ok {
			continue
		}
		if method == "PUT" || method == "POST" {
			res.EarnedPoints = 1
			break
		}
	}
	if res.EarnedPoints != 1 {
		res.Suggestions = append(res.Suggestions, "The operator should write into objects to update state. No PUT or POST requests from the operator were recorded by the scorecard.")
	}
	return res
}
