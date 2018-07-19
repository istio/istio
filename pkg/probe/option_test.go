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

package probe

import (
	"testing"
	"time"
)

func TestOption(t *testing.T) {
	var o *Options
	if o.IsValid() {
		t.Error("nil should not be valid")
	}
	o = &Options{}
	if o.IsValid() {
		t.Errorf("Empty option %+v should not be valid", o)
	}
	o.Path = "foo"
	if o.IsValid() {
		t.Errorf("%+v should not be valid since interval is missing", o)
	}
	o.UpdateInterval = time.Second
	if !o.IsValid() {
		t.Errorf("%+v should be valid", o)
	}
	o.Path = ""
	if o.IsValid() {
		t.Errorf("%+v should not be valid since path is missing", o)
	}
}
