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

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	sc "k8s.io/apimachinery/pkg/runtime/schema"
)

func TestInfo_APIResource(t *testing.T) {
	i := ResourceSpec{
		Version:  "version",
		Kind:     "kind",
		Group:    "group",
		ListKind: "listkind",
		Plural:   "plural",
		Singular: "singular",
	}
	r := i.APIResource()

	expected := &v1.APIResource{
		Group:        "group",
		Kind:         "kind",
		Version:      "version",
		Name:         "plural",
		Namespaced:   true,
		SingularName: "singular",
	}

	if !reflect.DeepEqual(r, expected) {
		t.Fatalf("Mismatch.\nActual:\n%v\nExpected:\n%v\n", r, expected)
	}
}

func TestInfo_GroupVersion(t *testing.T) {
	i := ResourceSpec{
		Version:  "version",
		Kind:     "kind",
		Group:    "group",
		ListKind: "listkind",
		Plural:   "plural",
		Singular: "singular",
	}
	gv := i.GroupVersion()

	expected := sc.GroupVersion{
		Group:   "group",
		Version: "version",
	}

	if !reflect.DeepEqual(gv, expected) {
		t.Fatalf("Mismatch.\nActual:\n%v\nExpected:\n%v\n", gv, expected)
	}
}

func TestInfo_CanonicalResourceName(t *testing.T) {
	i := ResourceSpec{
		Version:  "version",
		Kind:     "kind",
		Group:    "group",
		ListKind: "listkind",
		Plural:   "plural",
		Singular: "singular",
	}

	actual := i.CanonicalResourceName()
	expected := "plural.group/version"
	if actual != expected {
		t.Fatalf("Mismatch.\nActual:\n%q\nExpected:\n%q\n", actual, expected)
	}
}
