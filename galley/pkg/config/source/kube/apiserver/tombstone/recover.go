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

package tombstone

import (
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/stats"
)

// RecoverResource from a kubernetes tombstone (cache.DeletedFinalStateUnknown). Returns the resource or nil if
// recovery failed.
func RecoverResource(obj interface{}) metav1.Object {
	var tombstone cache.DeletedFinalStateUnknown
	var ok bool
	if tombstone, ok = obj.(cache.DeletedFinalStateUnknown); !ok {
		msg := fmt.Sprintf("error decoding object, invalid type: %v", reflect.TypeOf(obj))
		scope.Source.Error(msg)
		stats.RecordEventError(msg)
		return nil
	}

	var objectMeta metav1.Object
	if objectMeta, ok = tombstone.Obj.(metav1.Object); !ok {
		msg := fmt.Sprintf("error decoding object tombstone, invalid type: %v",
			reflect.TypeOf(tombstone.Obj))
		scope.Source.Error(msg)
		stats.RecordEventError(msg)
		return nil
	}

	scope.Source.Infof("Recovered deleted object '%s' from tombstone", objectMeta.GetName())
	return objectMeta
}
