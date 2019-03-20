// Copyright 2018 Istio Authors
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

package main

import (
	"strings"
)

// Uses proxyName to infer namespace if the passed proxyName contains namespace information.
// Otherwise uses the namespace value passed into the function
func inferPodInfo(proxyName, namespace string) (string, string) {
	parsedProxy := strings.Split(proxyName, ".")

	if len(parsedProxy) == 1 {
		return proxyName, namespace
	}
	return parsedProxy[0], parsedProxy[1]
}
