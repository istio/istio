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

package model

import (
	"fmt"

	security "istio.io/api/security/v1beta1"
)

func permissionTag(tag string) string {
	return fmt.Sprintf("MethodFromPermission[%s]", tag)
}

func principalTag(tag string) string {
	return fmt.Sprintf("UserFromPrincipal[%s]", tag)
}

func simplePrincipal(tag string) Principal {
	return Principal{
		Users: []string{principalTag(tag)},
	}
}

func newCondition(key string) *security.Condition {
	return &security.Condition{
		Key: key,
		Values: []string{
			fmt.Sprintf("value-%s-1", key),
			fmt.Sprintf("value-%s-2", key),
		},
		NotValues: []string{
			fmt.Sprintf("not-value-%s-1", key),
			fmt.Sprintf("not-value-%s-2", key),
		},
	}
}
