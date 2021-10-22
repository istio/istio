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

package test

import (
	"bytes"
	"encoding/json"
)

// JSONEquals compares two json strings. We cannot compare JSON strings from protobuf because of
// design decisions https://github.com/golang/protobuf/issues/1373 Instead, use this function to
// normalize the formatting
func JSONEquals(t Failer, a, b string) {
	t.Helper()
	ba := bytes.Buffer{}
	if err := json.Compact(&ba, []byte(a)); err != nil {
		t.Fatal(err)
	}
	bb := bytes.Buffer{}
	if err := json.Compact(&bb, []byte(b)); err != nil {
		t.Fatal(err)
	}
	if ba.String() != bb.String() {
		t.Fatalf("got %v, want %v", ba.String(), bb.String())
	}
}
