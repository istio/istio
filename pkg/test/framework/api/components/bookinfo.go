//  Copyright 2018 Istio Authors
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

package components

import (
	"testing"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

// Bookinfo represents a deployed Bookinfo app instance in a Kubernetes cluster.
type Bookinfo interface {
	component.Instance

	DeployRatingsV2(ctx context.Instance, scope lifecycle.Scope) error

	DeployMongoDb(ctx context.Instance, scope lifecycle.Scope) error
}

// GetBookinfo from the repository.
func GetBookinfo(e component.Repository, t testing.TB) Bookinfo {
	return e.GetComponentOrFail("", ids.BookInfo, t).(Bookinfo)
}
