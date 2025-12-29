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
	kerrors "k8s.io/apimachinery/pkg/api/errors"

	"istio.io/istio/pkg/kube/controllers"
)

func CreateOrUpdate[T controllers.Object](c Writer[T], object T) (T, error) {
	res, err := c.Create(object)
	if kerrors.IsAlreadyExists(err) {
		// Already exist, update
		return c.Update(object)
	}
	return res, err
}
