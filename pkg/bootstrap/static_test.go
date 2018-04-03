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

package bootstrap_test

import (
	"testing"

	"istio.io/istio/pkg/bootstrap"
)

func TestBuildBootstrap(t *testing.T) {
	zipkin := "zipkin:9411"

	telemetry := bootstrap.Upstream{
		ListenPort:   15004,
		UpstreamPort: 9091,
		GRPC:         true,
		Service:      "istio-telemetry",
		Auth:         false,
		UID:          "pod1.ns2",
		Operation:    "Report",
	}
	policy := bootstrap.Upstream{
		ListenPort:   15004,
		UpstreamPort: 9091,
		GRPC:         true,
		Service:      "istio-policy.ns4",
		Auth:         true,
		UID:          "pod2.ns2",
		Operation:    "Check",
	}
	discovery := bootstrap.Upstream{
		ListenPort:   15007,
		UpstreamPort: 8080,
		GRPC:         false,
		Service:      "istio-pilot",
		Auth:         true,
		UID:          "pod4.ns5",
		Operation:    "Discovery",
	}

	b1, berr := bootstrap.BuildBootstrap([]bootstrap.Upstream{telemetry}, &telemetry, zipkin, 15000)
	if berr != nil {
		t.Error(berr)
	}
	if err := b1.Validate(); err != nil {
		t.Errorf("invalid bootstrap %v: %#v", err, b1)
	}

	b2, berr := bootstrap.BuildBootstrap([]bootstrap.Upstream{policy}, nil, "", 15000)
	if berr != nil {
		t.Error(berr)
	}
	if err := b2.Validate(); err != nil {
		t.Errorf("invalid bootstrap %v: %#v", err, b2)
	}

	b3, berr := bootstrap.BuildBootstrap([]bootstrap.Upstream{discovery}, nil, "", 15000)
	if berr != nil {
		t.Error(berr)
	}
	if err := b3.Validate(); err != nil {
		t.Errorf("invalid bootstrap %v: %#v", err, b3)
	}

	b4, berr := bootstrap.BuildBootstrap(nil, nil, "", 15000)
	if berr != nil {
		t.Error(berr)
	}
	if err := b4.Validate(); err != nil {
		t.Errorf("invalid bootstrap %v: %#v", err, b4)
	}
}
