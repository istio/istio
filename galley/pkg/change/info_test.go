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

package change

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestInfo_String(t *testing.T) {
	cases := map[Info]string{
		{}:                          `Info[Type:Add, Name:, GroupVersion:]`,
		{Name: "foo"}:               `Info[Type:Add, Name:foo, GroupVersion:]`,
		{Name: "foo", Type: Update}: `Info[Type:Update, Name:foo, GroupVersion:]`,
		{Name: "foo", Type: Delete}: `Info[Type:Delete, Name:foo, GroupVersion:]`,
		{Name: "foo", Type: 4}:      `Info[Type:Unknown, Name:foo, GroupVersion:]`,
		{
			Name: "foo",
			Type: Add,
			GroupVersion: schema.GroupVersion{
				Group:   "g1",
				Version: "v1",
			},
		}: `Info[Type:Add, Name:foo, GroupVersion:g1/v1]`,
	}
	for k, v := range cases {
		actual := k.String()
		if strings.TrimSpace(actual) != strings.TrimSpace(v) {
			t.Fatalf("unexpected serialization: got:%v, wanted:%v", actual, v)
		}
	}
}
