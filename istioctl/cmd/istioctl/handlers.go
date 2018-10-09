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

func getProxyDetails(proxyName string) (string, string, error) {
	var ns string
	var err error

	parsedProxy := strings.Split(proxyName, ".")

	if len(parsedProxy) == 1 {
		ns, err = handleNamespaces("")
	} else {
		ns, err = handleNamespaces(parsedProxy[1])
	}
	if err != nil {
		return "", "", err
	} else {
		return parsedProxy[0], ns, err
	}
}
