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

package crd

import (
	"testing"
)

func TestValidator(t *testing.T) {
	validator := NewIstioValidator(t)
	t.Run("valid", func(t *testing.T) {
		if err := validator.ValidateCustomResourceYAML(`
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: STRICT
`); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		if err := validator.ValidateCustomResourceYAML(`
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: BAD
`); err == nil {
			t.Fatal("expected error but got none")
		}
	})
}
