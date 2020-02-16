//  Copyright 2020 Istio Authors
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

package promtheus

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework/components/prometheus"
)

func QueryPrometheus(t *testing.T, query string, promInst prometheus.Instance) error {
	t.Logf("query prometheus with: %v", query)
	val, err := promInst.WaitForQuiesce(query)
	if err != nil {
		return err
	}
	got, err := promInst.Sum(val, nil)
	if err != nil {
		t.Logf("value: %s", val.String())
		return fmt.Errorf("could not find metric value: %v", err)
	}
	t.Logf("get value %v", got)
	return nil
}
