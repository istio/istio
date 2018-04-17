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

package workload

import (
	"fmt"
	"testing"
)

func TestNewSecretServer_SecretFile(t *testing.T) {
	config := Config{
		Mode: 0,
	}
	actual, err := NewSecretServer(config)

	if err != nil {
		t.Errorf("server error")
	}

	if actual == nil {
		t.Errorf("secretServer should not be nil")
	}
}

func TestNewSecretServer_WorkloadAPI(t *testing.T) {
	config := Config{
		Mode: 1,
	}
	actual, err := NewSecretServer(config)

	if err != nil {
		t.Errorf("server error")
	}

	if actual == nil {
		t.Errorf("secretServer should not be nil")
	}
}

func TestNewSecretServer_Unsupported(t *testing.T) {
	config := Config{
		Mode: 2,
	}
	actual, err := NewSecretServer(config)

	expectedErr := fmt.Errorf("mode: 2 is not supported")
	if err.Error() != expectedErr.Error() {
		t.Errorf("error message mismatch")
	}

	if actual != nil {
		t.Errorf("server should be nil")
	}
}
