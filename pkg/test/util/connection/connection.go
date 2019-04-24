//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package connection

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework/components/apps"
)

const (
	AllowHTTPRespCode = "200"
	DenyHTTPRespCode  = "403"
	TCPPort           = 90
)

type Connection struct {
	From            apps.KubeApp
	To              apps.KubeApp
	Protocol        apps.AppProtocol
	Port            int
	Path            string
	ExpectedSuccess bool
}

func CheckConnection(t *testing.T, conn Connection) error {
	ep := conn.To.EndpointForPort(conn.Port)
	if ep == nil {
		return fmt.Errorf("cannot get upstream endpoint for connection test %v", conn)
	}

	results, err := conn.From.Call(ep, apps.AppCallOptions{Protocol: conn.Protocol, Path: conn.Path})
	if conn.ExpectedSuccess {
		if err != nil || len(results) == 0 || results[0].Code != AllowHTTPRespCode {
			// Addition log for debugging purpose.
			if err != nil {
				t.Logf("Error: %#v\n", err)
			} else if len(results) == 0 {
				t.Logf("No result\n")
			} else {
				t.Logf("Result: %v\n", results[0])
			}
			return fmt.Errorf("%s to %s:%d using %s: expected success, actually failed",
				conn.From.Name(), conn.To.Name(), conn.Port, conn.Protocol)
		}
	} else {
		if err == nil && len(results) > 0 && results[0].Code == AllowHTTPRespCode {
			return fmt.Errorf("%s to %s:%d using %s: expected failed, actually success",
				conn.From.Name(), conn.To.Name(), conn.Port, conn.Protocol)
		}
	}
	return nil
}
