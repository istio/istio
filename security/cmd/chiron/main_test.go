// Copyright 2019 Istio Authors
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
)

func TestIsMutatingWebhookConfiguration(t *testing.T) {
	testCases := map[string]struct {
		path       string
		shouldFail bool
		expectRet  bool
	}{
		"file not exist": {
			path:       "./invalid-path/invalid-file",
			shouldFail: true,
		},
		"valid mutating webhook config": {
			path:       "./test-data/example-mutating-webhook-config.yaml",
			shouldFail: false,
			expectRet:  true,
		},
		"invalid mutating webhook config": {
			path:       "./test-data/example-validating-webhook-config.yaml",
			shouldFail: false,
			expectRet:  false,
		},
	}

	for _, tc := range testCases {
		ret, err := isMutatingWebhookConfiguration(tc.path)
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed at isMutatingWebhookConfiguration()")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("failed at isMutatingWebhookConfiguration(): %v", err)
		}

		if tc.expectRet != ret {
			t.Error("the return value is unexpected")
		}
	}
}

func TestIsValidatingWebhookConfiguration(t *testing.T) {
	testCases := map[string]struct {
		path       string
		shouldFail bool
		expectRet  bool
	}{
		"file not exist": {
			path:       "./invalid-path/invalid-file",
			shouldFail: true,
		},
		"valid validating webhook config": {
			path:       "./test-data/example-validating-webhook-config.yaml",
			shouldFail: false,
			expectRet:  true,
		},
		"invalid validating webhook config": {
			path:       "./test-data/example-mutating-webhook-config.yaml",
			shouldFail: false,
			expectRet:  false,
		},
	}

	for _, tc := range testCases {
		ret, err := isValidatingWebhookConfiguration(tc.path)
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed at isValidatingWebhookConfiguration()")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("failed at isValidatingWebhookConfiguration(): %v", err)
		}

		if tc.expectRet != ret {
			t.Error("the return value is unexpected")
		}
	}
}
