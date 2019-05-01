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

package util

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/util/connection"
	"istio.io/istio/tests/util"
)

type TestCase struct {
	Request       connection.Connection
	ExpectAllowed bool
	Jwt           string
}

// CheckRBACRequest checks if a request is successful under RBAC policies.
// Under RBAC policies, a request is consider successful if:
// * If the policy is allow:
// *** Response code is 200
// * If the policy is deny:
// *** For HTTP: response code is 403.
// *** For TCP: EOF error and response code is 403.
func CheckRBACRequest(tc TestCase) error {
	req := tc.Request
	ep := req.To.EndpointForPort(req.Port)
	if ep == nil {
		return fmt.Errorf("cannot get upstream endpoint for connection test %v", req)
	}

	headers := make(http.Header)
	if len(tc.Jwt) > 0 {
		headers.Add("Authorization", "Bearer "+tc.Jwt)
	}

	resp, err := req.From.Call(ep, apps.AppCallOptions{Protocol: req.Protocol, Path: req.Path, Headers: headers})
	if err != nil && !strings.Contains(err.Error(), "EOF") {
		return fmt.Errorf("connection error with %v", err)
	}

	if tc.ExpectAllowed {
		if !(len(resp) > 0 && resp[0].Code == connection.HTTPCode200) {
			return fmt.Errorf("%s to %s:%d%s using %s: expected allow, actually deny",
				req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol)
		}
	} else {
		if req.Port == connection.TCPPort {
			if !(err != nil && strings.Contains(err.Error(), "EOF")) {
				return fmt.Errorf("%s to %s:%d%s using %s: expected deny with EOF error, actually %v",
					req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol, err)
			}
		} else {
			if !(len(resp) > 0 && resp[0].Code == connection.HTTPCode403) {
				return fmt.Errorf("%s to %s:%d%s using %s: expected deny, actually allow",
					req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol)
			}
		}
	}
	return nil
}

// GetRbacYamlFiles fills the template RBAC policy files with the given namespace and template files,
// writes the files to outDir and return the list of file paths.
// namespaceMapping is the mapping where the keys are the template variables in `rbacTmplFiles` and the values
// are the desired values to replace those template variables.
func GetRbacYamlFiles(t *testing.T, outDir string, namespaceMapping map[string]string, rbacTmplFiles []string) []string {
	var rbacYamlFiles []string
	for _, rbacTmplFile := range rbacTmplFiles {
		yamlFile, err := util.CreateAndFill(outDir, rbacTmplFile, namespaceMapping)
		if err != nil {
			t.Fatalf("Cannot create and fill %v", rbacTmplFile)
		}
		rbacYamlFiles = append(rbacYamlFiles, yamlFile)
	}
	return rbacYamlFiles
}
