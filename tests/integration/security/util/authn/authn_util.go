//go:build integ
// +build integ

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

package authn

import (
	"fmt"
	"net/http"

	"istio.io/istio/pkg/config/protocol"
	echoclient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
)

type TestCase struct {
	Name               string
	Config             string
	ExpectResponseCode int
	// Use empty value to express the header with such key must not exist.
	ExpectHeaders    map[string]string
	CallOpts         echo.CallOptions
	DestClusters     cluster.Clusters
	SkipMultiCluster bool
}

func (c *TestCase) String() string {
	return fmt.Sprintf("requests to %s%s expected code %d, headers %v",
		c.CallOpts.Target.Config().Service,
		c.CallOpts.Path,
		c.ExpectResponseCode,
		c.ExpectHeaders)
}

// CheckAuthn checks a request based on ExpectResponseCode.
func (c *TestCase) CheckAuthn(responses echoclient.Responses, err error) error {
	return check.And(
		check.StatusCode(c.ExpectResponseCode),
		check.RequestHeaders(c.ExpectHeaders),
		check.Each(func(r echoclient.Response) error {
			if c.ExpectResponseCode == http.StatusOK && c.DestClusters.IsMulticluster() {
				return check.ReachedClusters(c.DestClusters).Check(responses, nil)
			}
			return nil
		})).Check(responses, err)
}

// CheckIngressOrFail checks a request for the ingress gateway.
func CheckIngressOrFail(ctx framework.TestContext, ingr ingress.Instance, host string, path string,
	headers map[string][]string, token string, expectResponseCode int) {
	if headers == nil {
		headers = map[string][]string{
			"Host": {host},
		}
	} else {
		headers["Host"] = []string{host}
	}
	opts := echo.CallOptions{
		Port: &echo.Port{
			Protocol: protocol.HTTP,
		},
		Path:    path,
		Headers: headers,
		Check:   check.StatusCode(expectResponseCode),
	}
	if len(token) != 0 {
		opts.Headers["Authorization"] = []string{
			fmt.Sprintf("Bearer %s", token),
		}
	}
	ingr.CallWithRetryOrFail(ctx, opts)
}
