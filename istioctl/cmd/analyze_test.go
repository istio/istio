// Copyright 2019 Istio Authors.
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

package cmd

import (
	"io/ioutil"
	"testing"

	"istio.io/istio/galley/pkg/config/analysis/diag"
)

func TestErrorOnIssuesFound(t *testing.T) {
	msgs := []diag.Message{
		diag.NewMessage(
			diag.NewMessageType(diag.Error, "B1", "Template: %q"),
			nil,
			"",
		),
		diag.NewMessage(
			diag.NewMessageType(diag.Warning, "A1", "Template: %q"),
			nil,
			"",
		),
	}

	err := handleAnalyzeMessages(ioutil.Discard, msgs)

	switch err := err.(type) {
	case FoundAnalyzeIssuesError:
		if len(msgs) != err.int {
			t.Errorf("Expected %v errors, but got %v.", len(msgs), err.int)
		}
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", err)
		return
	}
}

func TestNoErrorOnNoIssuesFound(t *testing.T) {
	msgs := []diag.Message{
		diag.NewMessage(
			diag.NewMessageType(diag.Info, "B1", "Template: %q"),
			nil,
			"",
		),
		diag.NewMessage(
			diag.NewMessageType(diag.Info, "A1", "Template: %q"),
			nil,
			"",
		),
	}

	err := handleAnalyzeMessages(ioutil.Discard, msgs)

	if err != nil {
		t.Errorf("Expected nil, but got %q.", err)
	}
}
