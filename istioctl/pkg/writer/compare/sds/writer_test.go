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

package sdscompare

import (
	"bytes"
	"strings"
	"testing"
)

func TestSDSWriterSecretItems(t *testing.T) {
	tests := []struct {
		name       string
		format     Format
		items      []SecretItem
		expected   []string
		unexpected []string
	}{
		{
			name:       "test tabular output with no secret items is equivalent to the header",
			format:     TABULAR,
			items:      []SecretItem{},
			expected:   []string{},
			unexpected: secretItemColumns,
		},
		{
			name:   "test tabular output with a single secret item",
			format: TABULAR,
			items: []SecretItem{
				{
					Name:        "olinger",
					Data:        "certdata",
					Source:      "source",
					Destination: "destination",
					SecretMeta: SecretMeta{
						Valid:        true,
						SerialNumber: "serial_number",
						NotAfter:     "expires",
						NotBefore:    "valid",
						Type:         "type",
					},
				},
			},
			expected: append(
				[]string{"olinger", "serial_number", "expires", "valid", "type"},
				secretItemColumns...),
			unexpected: []string{"source", "destination", "certdata"},
		},
		{
			name:   "test JSON output with a single secret item",
			format: JSON,
			items: []SecretItem{
				{
					Name:        "olinger",
					Data:        "certdata",
					Source:      "source",
					Destination: "destination",
					SecretMeta: SecretMeta{
						Valid:        true,
						SerialNumber: "serial_number",
						NotAfter:     "expires",
						NotBefore:    "valid",
						Type:         "type",
					},
				},
			},
			expected: []string{"olinger", "source", "destination", "serial_number", "expires", "valid", "type", "certdata"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			mockWriter := NewSDSWriter(w, tt.format)
			err := mockWriter.PrintSecretItems(tt.items)
			if err != nil {
				t.Errorf("error printing secret items: %v", err)
			}
			checkOutput(t, w.String(), tt.expected, tt.unexpected)
		})
	}
}

func TestSDSWriterSecretDiff(t *testing.T) {
	tests := []struct {
		name       string
		format     Format
		diffs      []SecretItemDiff
		expected   []string
		unexpected []string
	}{
		{
			name:       "test tabular output with no secret items is equivalent to the header",
			format:     TABULAR,
			diffs:      []SecretItemDiff{},
			expected:   []string{},
			unexpected: secretDiffColumns,
		},
		{
			name:   "test tabular output with a single secret diff",
			format: TABULAR,
			diffs: []SecretItemDiff{
				{
					Agent: "alligator",
					Proxy: "proxy",
					SecretItem: SecretItem{
						Name:        "fields",
						Data:        "certdata",
						Source:      "should",
						Destination: "destination",
						SecretMeta: SecretMeta{
							Valid:        true,
							SerialNumber: "serial_number",
							NotAfter:     "expires",
							NotBefore:    "valid",
							Type:         "type",
						},
					},
				},
			},
			expected: append(
				[]string{"fields", "should", "serial_number", "expires", "valid", "type", "proxy"},
				secretDiffColumns...),
			unexpected: []string{"alligator", "certdata"},
		},
		{
			name:   "test JSON output with a single secret diff",
			format: JSON,
			diffs: []SecretItemDiff{
				{
					Agent: "alligator",
					Proxy: "proxy",
					SecretItem: SecretItem{
						Name:        "fields",
						Data:        "certdata",
						Source:      "should",
						Destination: "destination",
						SecretMeta: SecretMeta{
							Valid:        true,
							SerialNumber: "serial_number",
							NotAfter:     "expires",
							NotBefore:    "valid",
							Type:         "type",
						},
					},
				},
			},
			expected: []string{"fields", "should", "serial_number", "expires", "valid", "type", "proxy", "certdata"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			mockWriter := NewSDSWriter(w, tt.format)
			err := mockWriter.PrintDiffs(tt.diffs)
			if err != nil {
				t.Errorf("error printing secret items: %v", err)
			}
			checkOutput(t, w.String(), tt.expected, tt.unexpected)
		})
	}
}

func checkOutput(t *testing.T, output string, expected, unexpected []string) {
	t.Helper()
	for _, expected := range expected {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %s included in writer output, did not find", expected)
		}
	}
	for _, unexpected := range unexpected {
		if strings.Contains(output, unexpected) {
			t.Errorf("unexpected string %s included in writer output", unexpected)
		}
	}
}
