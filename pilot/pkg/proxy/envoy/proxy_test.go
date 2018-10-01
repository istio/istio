// Copyright 2018 Istio Authors
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

package envoy

import (
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

func TestEnvoyArgs(t *testing.T) {
	config := model.DefaultProxyConfig()
	config.ServiceCluster = "my-cluster"
	config.Concurrency = 8

	test := &envoy{config: config, node: "my-node", extraArgs: []string{"-l", "trace"}}
	testProxy := NewProxy(config, "my-node", "trace", nil)
	if !reflect.DeepEqual(testProxy, test) {
		t.Errorf("unexpected struct got\n%v\nwant\n%v", testProxy, test)
	}

	got := test.args("test.json", 5)
	want := []string{
		"-c", "test.json",
		"--restart-epoch", "5",
		"--drain-time-s", "2",
		"--parent-shutdown-time-s", "3",
		"--service-cluster", "my-cluster",
		"--service-node", "my-node",
		"--max-obj-name-len", fmt.Sprint(config.StatNameLength),
		"--allow-unknown-fields",
		"-l", "trace",
		"--concurrency", "8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("envoyArgs() => got %v, want %v", got, want)
	}
}

// TestEnvoyRun is no longer used - we are now using v2 bootstrap API.
