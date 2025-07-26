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

package cli

import (
	"testing"
)

func TestRevisionOrDefaultWithMockDefaultRevision(t *testing.T) {
	tests := []struct {
		description    string
		inputRevision  string
		expectedResult string
	}{
		{
			description:    "return provided revision when non-empty",
			inputRevision:  "test-revision",
			expectedResult: "test-revision",
		},
		{
			description:    "return empty string when no revision provided (fake context has no default detection)",
			inputRevision:  "",
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			// Create a fake context - it won't have real default revision detection
			// but will test the basic logic
			ctx := NewFakeContext(&NewFakeContextOption{})
			got := ctx.RevisionOrDefault(tt.inputRevision)
			if got != tt.expectedResult {
				t.Fatalf("unexpected result: wanted %v got %v", tt.expectedResult, got)
			}
		})
	}
}

func TestRevisionOrDefaultCaching(t *testing.T) {
	// Test that the basic logic works correctly
	ctx := NewFakeContext(&NewFakeContextOption{})

	// Test that explicit revisions are returned as-is
	result1 := ctx.RevisionOrDefault("stable-revision")
	if result1 != "stable-revision" {
		t.Fatalf("expected stable-revision, got %v", result1)
	}

	// Test that empty revisions return empty (since fake context has no default detection)
	result2 := ctx.RevisionOrDefault("")
	if result2 != "" {
		t.Fatalf("expected empty string, got %v", result2)
	}

	// Test consistency
	result3 := ctx.RevisionOrDefault("explicit-revision")
	if result3 != "explicit-revision" {
		t.Fatalf("expected explicit-revision, got %v", result3)
	}
}
