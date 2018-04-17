// Copyright 2017 Istio Authors
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

package osb

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

func TestAddService(t *testing.T) {
	c := new(Catalog)
	s := &Service{
		Name: "test service",
	}
	c.AddService(s)

	for _, got := range c.Services {
		if !reflect.DeepEqual(got, *s) {
			t.Errorf("failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(s))
		}
	}
}
