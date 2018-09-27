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

package metadata

import (
	"testing"

	pgogo "github.com/gogo/protobuf/proto"
)

func TestTypes_Info(t *testing.T) {
	for _, info := range Types.All() {
		i, found := Types.Lookup(info.TypeURL.String())
		if !found {
			t.Fatalf("Unable to find by lookup: %q", info.TypeURL.String())
		}
		if i != info {
			t.Fatalf("Lookup mismatch. Expected:%v, Actual:%v", info, i)
		}
	}
}

func TestTypes_NewProtoInstance(t *testing.T) {
	for _, info := range Types.All() {
		p := info.NewProtoInstance()
		name := pgogo.MessageName(p)
		if name != info.TypeURL.MessageName() {
			t.Fatalf("Name/TypeURL mismatch: TypeURL:%v, Name:%v", info.TypeURL, name)
		}
	}
}

func TestTypes_LookupByTypeURL(t *testing.T) {
	for _, info := range Types.All() {
		i, found := Types.Lookup(info.TypeURL.String())

		if !found {
			t.Fatalf("Expected info not found: %v", info)
		}

		if i != info {
			t.Fatalf("Lookup mismatch. Expected:%v, Actual:%v", info, i)
		}
	}
}

func TestTypes_TypeURLs(t *testing.T) {
	for _, url := range Types.TypeURLs() {
		_, found := Types.Lookup(url)

		if !found {
			t.Fatalf("Expected info not found: %v", url)
		}
	}
}

func TestTypes_Lookup(t *testing.T) {
	for _, info := range Types.All() {
		if _, found := Types.Lookup(info.TypeURL.String()); !found {
			t.Fatalf("expected info not found for: %s", info.TypeURL.String())
		}
	}
}
