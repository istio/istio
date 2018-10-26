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

package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestCreateConfig(t *testing.T) {
	k := kube{
		cfg: &rest.Config{},
	}
	gv := schema.GroupVersion{Group: "group", Version: "version"}

	_, err := k.DynamicInterface(gv, "kind", "listkind")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestNewKube(t *testing.T) {
	// Should not panic
	_ = NewKube(&rest.Config{})
}
