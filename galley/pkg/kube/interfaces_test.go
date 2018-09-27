//  Copyright 2018 Istio Authors
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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestAddTypesToScheme(t *testing.T) {
	gv := schema.GroupVersion{Group: "group", Version: "version"}
	s := runtime.NewScheme()

	err := addTypeToScheme(s, gv, "kind", "listkind")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	_, err = s.New(schema.GroupVersionKind{Group: "group", Version: "version", Kind: "kind"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestCreateConfig(t *testing.T) {
	k := kube{
		cfg: &rest.Config{},
	}
	gv := schema.GroupVersion{Group: "group", Version: "version"}

	cfg, err := k.createConfig(gv, "kind", "listkind")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if *cfg.GroupVersion != gv {
		t.Fatalf("GroupVersion mismatch:\nActual:\n%v\nExpected:\n%v\n", *cfg.GroupVersion, gv)
	}
}

func TestNewKube(t *testing.T) {
	// Should not panic
	_ = NewKube(&rest.Config{})
}
