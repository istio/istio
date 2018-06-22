//  Copyright 2018 Istio Authors
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

package kube

import (
	"reflect"
	"testing"
)

func TestEntries_All(t *testing.T) {
	e := &Schema{}

	i1 := ResourceSpec{Kind: "foo"}
	i2 := ResourceSpec{Kind: "bar"}

	e.entries = append(e.entries, i1)
	e.entries = append(e.entries, i2)

	r := e.All()

	expected := []ResourceSpec{i1, i2}
	if !reflect.DeepEqual(expected, r) {
		t.Fatalf("Mismatch Expected:\n%v\nActual:\n%v\n", expected, r)
	}
}

func TestGetTargetFor(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Should have panicked")
		}
	}()

	getTargetFor("shazbat")
}
