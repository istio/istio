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
)

func TestAll(t *testing.T) {
	listChecker, err := newInstance(&InstanceConfig{})
	if err != nil {
		t.Error("Unable to create instance")
	}

	//	listChecker := inst.(adapters.ListChecker)

	var ok bool
	ok, err = listChecker.CheckList("")
	if ok || err != nil {
		t.Error("Expecting to always get false")
	}

	ok, err = listChecker.CheckList("ABC")
	if ok || err != nil {
		t.Error("Expecting to always get false")
	}

	listChecker.Delete()
}
