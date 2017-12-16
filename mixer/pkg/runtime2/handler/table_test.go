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

package handler

import (
	"fmt"
	"testing"

	"istio.io/istio/mixer/pkg/runtime2/testing/data"
)

func TestTable_GetHealthyHandler(t *testing.T) {
	table := &Table{
		entries: make(map[string]entry),
	}

	table.entries["h1"] = entry{
		Name:    "h1",
		Handler: &data.FakeHandler{},
	}

	h, found := table.GetHealthyHandler("h1")
	if !found {
		t.Fail()
	}

	if h == nil {
		t.Fail()
	}
}

func TestTable_GetHealthyHandler_Empty(t *testing.T) {
	table := &Table{
		entries: make(map[string]entry),
	}

	_, found := table.GetHealthyHandler("h1")
	if found {
		t.Fail()
	}
}

func TestTable_GetHealthyHandler_Unhealthy(t *testing.T) {
	table := &Table{
		entries: make(map[string]entry),
	}

	table.entries["h1"] = entry{
		Name:         "h1",
		Handler:      &data.FakeHandler{},
		StartupError: fmt.Errorf("cheese not found"),
	}

	_, found := table.GetHealthyHandler("h1")
	if found {
		t.Fail()
	}
}

func TestEmpty(t *testing.T) {
	table := Empty()

	if len(table.entries) > 0 {
		t.Fail()
	}
}
