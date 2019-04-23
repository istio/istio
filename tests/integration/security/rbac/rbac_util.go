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

package rbac

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"istio.io/istio/pkg/log"

	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/util/connection"
	"istio.io/istio/tests/util"
)

type testCase struct {
	request       connection.Connection
	expectAllowed bool
	jwt           string
}

// checkRBACRequest checks if a request is successful under RBAC policies.
// Under RBAC policies, a request is consider successful if:
// * If the policy is deny:
// *** For HTTP: response code is same as the rejectionCode input parameter.
// *** For TCP: EOF error
// * If the policy is allow:
// *** Response code is 200
func checkRBACRequest(tc testCase, rejectionCode string) error {
	req := tc.request
	ep := req.To.EndpointForPort(req.Port)
	if ep == nil {
		return fmt.Errorf("cannot get upstream endpoint for connection test %v", req)
	}

	headers := make(http.Header)
	if len(tc.jwt) > 0 {
		headers.Add("Authorization", "Bearer "+tc.jwt)
	}

	resp, err := req.From.Call(ep, apps.AppCallOptions{Protocol: req.Protocol, Path: req.Path, Headers: headers})
	if err != nil && !strings.Contains(err.Error(), "EOF") {
		return fmt.Errorf("connection error with %v", err)
	}

	if len(resp) > 0 {
		log.Infof("%s to %s:%d%s using %s: expectAllowed %v, response code %v",
			req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol, tc.expectAllowed, resp[0].Code)
	} else {
		log.Infof("%s to %s:%d%s using %s: expectAllowed %v, empty response",
			req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol, tc.expectAllowed)
	}
	if tc.expectAllowed {
		if !(len(resp) > 0 && resp[0].Code == connection.AllowHTTPRespCode) {
			return fmt.Errorf("%s to %s:%d%s using %s: expected allow, actually deny",
				req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol)
		}
	} else {
		if req.Port == connection.TCPPort {
			if !strings.Contains(err.Error(), "EOF") {
				return fmt.Errorf("%s to %s:%d%s using %s: expected deny with EOF error, actually %v",
					req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol, err)
			}
		} else {
			if !(len(resp) > 0 && resp[0].Code == rejectionCode) {
				return fmt.Errorf("%s to %s:%d%s using %s: expected deny, actually allow",
					req.From.Name(), req.To.Name(), req.Port, req.Path, req.Protocol)
			}
		}
	}
	// Success
	return nil
}

// getRbacYamlFiles fills the template RBAC policy files with the given namespace and template files,
// writes the files to outDir and return the list of file paths.
func getRbacYamlFiles(t *testing.T, outDir, namespace string, rbacTmplFiles []string) []string {
	var rbacYamlFiles []string
	namespaceParams := map[string]string{
		"Namespace": namespace,
	}
	for _, rbacTmplFile := range rbacTmplFiles {
		yamlFile, err := util.CreateAndFill(outDir, rbacTmplFile, namespaceParams)
		if err != nil {
			t.Fatalf("Cannot create and fill %v", rbacTmplFile)
		}
		rbacYamlFiles = append(rbacYamlFiles, yamlFile)
	}
	return rbacYamlFiles
}
