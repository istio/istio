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

package kclient

import (
	"reflect"

	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/informermetric"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/ptr"
	"istio.io/pkg/log"
)

type Untyped = Reader[controllers.Object]

// NewUntyped returns an untyped client for a given informer. This is read-only.
func NewUntyped(c kube.Client, inf cache.SharedIndexInformer, filter Filter) Untyped {
	return ptr.Of(newReadClient[controllers.Object](c, inf, filter))
}

func newReadClient[T controllers.Object](c kube.Client, inf cache.SharedIndexInformer, filter Filter) readClient[T] {
	i := *new(T)
	t := reflect.TypeOf(i)
	if err := c.RegisterFilter(t, filter); err != nil {
		if features.EnableUnsafeAssertions {
			log.Fatal(err)
		}
		log.Warn(err)
	}
	if filter.ObjectTransform != nil {
		_ = inf.SetTransform(filter.ObjectTransform)
	} else {
		_ = inf.SetTransform(kube.StripUnusedFields)
	}

	if err := inf.SetWatchErrorHandler(informermetric.ErrorHandlerForCluster(c.ClusterID())); err != nil {
		log.Debugf("failed to set watch handler, informer may already be started: %v", err)
	}
	return readClient[T]{
		informer: inf,
		filter:   filter.ObjectFilter,
	}
}
