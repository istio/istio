// +build integ
//  Copyright Istio Authors
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

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

// Checker is a test utility for testing the network connectivity between two endpoints.
type Checker struct {
	From          echo.Instance
	DestClusters  cluster.Clusters
	Options       echo.CallOptions
	ExpectSuccess bool
	ExpectMTLS    bool
}

// Check whether the target endpoint is reachable from the source.
func (c *Checker) Check() error {
	results, err := c.From.Call(c.Options)
	if c.ExpectSuccess {
		if err == nil {
			err = results.CheckOK()
		}
		if err != nil {
			return fmt.Errorf("%s to %s:%s using %s: expected success but failed: %v",
				c.From.Config().Service, c.Options.Target.Config().Service, c.Options.PortName, c.Options.Scheme, err)
		}
		// TODO: check why grpc can not reach all clusters
		if c.DestClusters.IsMulticluster() && c.Options.Scheme != scheme.GRPC && c.Options.Count > 1 {
			err = results.CheckReachedClusters(c.DestClusters)
			if err != nil {
				return err
			}
		}
		if c.ExpectMTLS {
			err := results.CheckMTLSForHTTP()
			gotMtls := err == nil
			if gotMtls != c.ExpectMTLS {
				return fmt.Errorf("%s to %s:%s using %s: expected mtls=%v, got mtls=%v",
					c.From.Config().Service, c.Options.Target.Config().Service, c.Options.PortName, c.Options.Scheme, c.ExpectMTLS, gotMtls)
			}
		}
		return nil
	}

	// Expect failure...
	if err == nil && results.CheckOK() == nil {
		return fmt.Errorf("%s to %s:%s using %s: expected failed, actually success",
			c.From.Config().Service, c.Options.Target.Config().Service, c.Options.PortName, c.Options.Scheme)
	}
	return nil
}

func (c *Checker) CheckOrFail(t test.Failer) {
	if err := retry.UntilSuccess(c.Check, echo.DefaultCallRetryOptions()...); err != nil {
		t.Fatal(err)
	}
}
