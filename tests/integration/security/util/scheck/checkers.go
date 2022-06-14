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

package scheck

import (
	"errors"
	"net/http"
	"strconv"

	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
)

func NotOK() echo.Checker {
	strCode := strconv.Itoa(http.StatusOK)
	return check.Or(check.Error(), check.Each(func(r echoClient.Response) error {
		if r.Code == strCode {
			return errors.New("response status code was 100 (OK), but expected failure")
		}
		return nil
	}))
}

func ReachedClusters(allClusters cluster.Clusters, opts *echo.CallOptions) echo.Checker {
	// TODO(https://github.com/istio/istio/issues/37307): Investigate why we don't reach all clusters.
	if opts.To.Clusters().IsMulticluster() && opts.Count > 1 && opts.Scheme != scheme.GRPC && !opts.To.Config().IsHeadless() {
		return check.ReachedTargetClusters(allClusters)
	}
	return echo.NoChecker()
}
