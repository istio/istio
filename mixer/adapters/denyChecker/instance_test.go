// Copyright 2016 Google Inc.
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

package denyChecker

import (
	"testing"

	"github.com/istio/mixer/adapters"
)

func TestAll(t *testing.T) {
	var inst adapters.Instance
	var err error
	if inst, err = newInstance(&InstanceConfig{}); err != nil {
		t.Error("Unable to create instance")
	}

	listChecker := inst.(adapters.ListChecker)
	if listChecker.CheckList("") || listChecker.CheckList("ABC") {
		t.Error("Expecting to always get false")
	}

	inst.Delete()
}
