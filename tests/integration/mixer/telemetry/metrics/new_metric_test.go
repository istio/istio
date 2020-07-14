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

package metrics

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
)

func TestNewMetric(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			ctx.Config().ApplyYAMLOrFail(ctx, "",
				bookinfo.DoubleMetric.LoadOrFail(ctx))
			defer ctx.Config().DeleteYAMLOrFail(ctx, "",
				bookinfo.DoubleMetric.LoadOrFail(ctx))

			util.AllowRuleSync(t)

			addr := ing.HTTPAddress()
			url := fmt.Sprintf("http://%s/productpage", addr.String())
			retry.UntilSuccessOrFail(t, func() error {
				res := util.SendTraffic(ing, t, "Sending traffic", url, "", 10)
				if res.RetCodes[200] < 1 {
					return fmt.Errorf("unable to retrieve 200 from product page: %v", res.RetCodes)
				}
				return nil
			}, retry.Delay(time.Second))

			// We are expecting double request count, so we should see at least 2 requests here
			util.ValidateMetric(t, prom, "sum(istio_double_request_count{message=\"twice the fun!\"})", "istio_double_request_count", 2)
		})
}
