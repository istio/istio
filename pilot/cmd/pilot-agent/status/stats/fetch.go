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

package stats

import (
	"bytes"
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/cmd/pilot-agent/status/util"
)

// Fetch stats from Envoy and return the buffer.
func Fetch(proxyAdminPort uint16) (*bytes.Buffer, error) {
	buf, err := util.DoHTTPGet(fmt.Sprintf("http://127.0.0.1:%d/stats?usedonly", proxyAdminPort))
	if err != nil {
		return nil, multierror.Prefix(err, "failed retrieving Envoy stats:")
	}
	return buf, nil
}
