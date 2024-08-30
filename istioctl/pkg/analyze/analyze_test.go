// Copyright Istio Authors.
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

package analyze

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/testutil"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/test/util/assert"
)

func TestErrorOnIssuesFound(t *testing.T) {
	g := NewWithT(t)

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

	err := errorIfMessagesExceedThreshold(msgs)

	g.Expect(err).To(BeIdenticalTo(AnalyzerFoundIssuesError{}))
}

func TestNoErrorIfMessageLevelsBelowThreshold(t *testing.T) {
	g := NewWithT(t)

	msgs := []diag.Message{
		diag.NewMessage(
			diag.NewMessageType(diag.Info, "B1", "Template: %q"),
			nil,
			"",
		),
		diag.NewMessage(
			diag.NewMessageType(diag.Warning, "A1", "Template: %q"),
			nil,
			"",
		),
	}

	err := errorIfMessagesExceedThreshold(msgs)

	g.Expect(err).To(BeNil())
}

func TestSkipPodsInFiles(t *testing.T) {
	c := testutil.TestCase{
		Args: strings.Split(
			"-A --use-kube=false --failure-threshold ERROR testdata/analyze-file/public-gateway.yaml",
			" "),
		WantException: false,
	}
	analyze := Analyze(cli.NewFakeContext(nil))
	testutil.VerifyOutput(t, analyze, c)
}

func TestIsValidFile(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		expected bool
	}{
		{
			name:     "Test .json extension",
			fileName: "abc.json",
			expected: true,
		},
		{
			name:     "Test .yaml extension",
			fileName: "abc.yaml",
			expected: true,
		},
		{
			name:     "Test .yml extension",
			fileName: "abc.yml",
			expected: true,
		},
		{
			name:     "Test invalid extension",
			fileName: "abc.tsc",
			expected: false,
		},
		{
			name:     "Test empty extension",
			fileName: "abc",
			expected: false,
		},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.name), func(t *testing.T) {
			assert.Equal(t, c.expected, isValidFile(c.fileName))
		})
	}
}
