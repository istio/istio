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

package traffic

import (
	"bytes"
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/echo/client"
)

// Result of a traffic generation operation.
type Result struct {
	TotalRequests      int
	SuccessfulRequests int
	Error              error
}

func (r Result) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "TotalRequests:       %d\n", r.TotalRequests)
	_, _ = fmt.Fprintf(buf, "SuccessfulRequests:  %d\n", r.SuccessfulRequests)
	_, _ = fmt.Fprintf(buf, "PercentSuccess:      %f\n", r.PercentSuccess())
	_, _ = fmt.Fprintf(buf, "Errors:              %v\n", r.Error)

	return buf.String()
}

func (r *Result) add(resp client.ParsedResponses, err error) {
	count := resp.Len()
	if count == 0 {
		count = 1
	}

	r.TotalRequests += count
	if err != nil {
		r.Error = multierror.Append(r.Error, err)
	} else {
		r.SuccessfulRequests += count
	}
}

func (r Result) PercentSuccess() float64 {
	return (float64(r.SuccessfulRequests) / float64(r.TotalRequests)) * 100.0
}
