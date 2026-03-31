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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"istio.io/istio/pkg/test/echo/proto"
)

func TestGetRequest_HeaderWithColon(t *testing.T) {
	tests := []struct {
		name          string
		headers       []string
		expectError   bool
		expectedPairs []*proto.Header
	}{
		{
			name:        "Common header format - no colon in value",
			headers:     []string{"X-Test:value"},
			expectError: false,
			expectedPairs: []*proto.Header{
				{Key: "X-Test", Value: "value"},
			},
		},
		{
			name:        "Single colon in value",
			headers:     []string{"X-Test: value:with:colon"},
			expectError: false,
			expectedPairs: []*proto.Header{
				{Key: "X-Test", Value: "value:with:colon"},
			},
		},
		{
			name:        "Multiple colons in value",
			headers:     []string{"X-Multi: val1:val2:val3"},
			expectError: false,
			expectedPairs: []*proto.Header{
				{Key: "X-Multi", Value: "val1:val2:val3"},
			},
		},
		{
			name:        "Missing colon - wrong format",
			headers:     []string{"InvalidHeader"},
			expectError: true,
		},
		{
			name:        "Multiple valid headers",
			headers:     []string{"X-Multi: val1:val2:val3", "X-Multi2: val1", "X-Multi3: val1:val2"},
			expectError: false,
			expectedPairs: []*proto.Header{
				{Key: "X-Multi", Value: "val1:val2:val3"},
				{Key: "X-Multi2", Value: "val1"},
				{Key: "X-Multi3", Value: "val1:val2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers = tt.headers

			req, err := getRequest("example.com")
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Len(t, req.Headers, len(tt.expectedPairs))

			for i, expected := range tt.expectedPairs {
				assert.Equal(t, expected.Key, req.Headers[i].Key)
				assert.Equal(t, expected.Value, req.Headers[i].Value)
			}
		})
	}
}
