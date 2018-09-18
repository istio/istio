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

package validation

import (
	"strings"
	"testing"
)

// scenario is a common struct used by many tests in this context.
type scenario struct {
	name          string
	wrapFunc      func(*WebhookParameters)
	expectedError string
}

func TestValidate(t *testing.T) {
	scenarios := map[string]scenario{
		"valid": {
			wrapFunc:      func(args *WebhookParameters) {},
			expectedError: "",
		},
		"invalid deployment namespace": {
			wrapFunc:      func(args *WebhookParameters) { args.DeploymentNamespace = "_/invalid" },
			expectedError: "invalid deployment namespace: \"_/invalid\"",
		},
		"invalid deployment name": {
			wrapFunc:      func(args *WebhookParameters) { args.DeploymentName = "_/invalid" },
			expectedError: "invalid deployment name: \"_/invalid\"",
		},
		"webhook unset": {
			wrapFunc:      func(args *WebhookParameters) { args.WebhookConfigFile = "" },
			expectedError: "webhookConfigFile not specified",
		},
		"cert unset": {
			wrapFunc:      func(args *WebhookParameters) { args.CertFile = "" },
			expectedError: "cert file not specified",
		},
		"key unset": {
			wrapFunc:      func(args *WebhookParameters) { args.KeyFile = "" },
			expectedError: "key file not specified",
		},
		"ca cert unset": {
			wrapFunc:      func(args *WebhookParameters) { args.CACertFile = "" },
			expectedError: "CA cert file not specified",
		},
		"invalid port": {
			wrapFunc:      func(args *WebhookParameters) { args.Port = 100000 },
			expectedError: "port number 100000 must be in the range 1..65535",
		},
	}

	for name, scenario := range scenarios {
		t.Run(name, func(tt *testing.T) {
			runTestCode(name, tt, scenario)
		})
	}
}

func runTestCode(name string, t *testing.T, test scenario) {
	args := DefaultArgs()
	// wrap the args with a webhook config file.
	args.WebhookConfigFile = "/etc/istio/config/validatingwebhookconfiguration.yaml"

	test.wrapFunc(args)
	err := args.Validate()
	if err == nil && test.expectedError != "" {
		t.Errorf("Test %q failed: expected error: %q, got nil", name, test.expectedError)
	}
	if err != nil {
		if test.expectedError == "" {
			t.Errorf("Test %q failed: expected nil error, got %v", name, err)
		}
		if !strings.HasSuffix(err.Error(), test.expectedError) {
			t.Errorf("Test %q failed: expected error: %q, got %q", name, test.expectedError, err.Error())
		}
	}

	// Should not return error if validation disabled
	args.EnableValidation = false
	if err := args.Validate(); err != nil {
		t.Errorf("Test %q failed with validation disabled, expected nil error, but got: %v", name, err)
	}
}
