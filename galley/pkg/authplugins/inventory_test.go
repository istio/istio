// Copyright 2019 Istio Authors
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

package authplugins_test

import (
	"testing"

	"istio.io/istio/galley/pkg/authplugins"
)

func TestInventory(t *testing.T) {
	for _, v := range authplugins.Inventory() {
		if v == nil {
			t.Error("Invalid GetInfo function")
		}
	}
}

func TestAuthMap(t *testing.T) {
	for k, v := range authplugins.AuthMap() {
		if k == "" {
			t.Error("Empty plugin name")
		}
		if v == nil {
			t.Error("Invalid GetAuth function")
		}
	}
}
