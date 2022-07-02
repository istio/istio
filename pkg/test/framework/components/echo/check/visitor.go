//  Copyright Istio Authors
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

package check

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/util/istiomultierror"
)

// Visitor is performs a partial check operation on a single message.
type Visitor func(echoClient.Response) error

// Visit is a utility method that just invokes this Visitor function on the given response.
func (v Visitor) Visit(r echoClient.Response) error {
	return v(r)
}

// And returns a Visitor that performs a logical AND of this Visitor and the one provided.
func (v Visitor) And(o Visitor) Visitor {
	return func(r echoClient.Response) error {
		if err := v(r); err != nil {
			return err
		}
		return o(r)
	}
}

// Or returns a Visitor that performs a logical OR of this Visitor and the one provided.
func (v Visitor) Or(o Visitor) Visitor {
	return func(r echoClient.Response) error {
		if err := v(r); err != nil {
			return err
		}
		return o(r)
	}
}

// Checker returns an echo.Checker based on this Visitor.
func (v Visitor) Checker() echo.Checker {
	return func(result echo.CallResult, _ error) error {
		rs := result.Responses
		if rs.IsEmpty() {
			return fmt.Errorf("no responses received")
		}
		outErr := istiomultierror.New()
		for i, r := range rs {
			if err := v.Visit(r); err != nil {
				outErr = multierror.Append(outErr, fmt.Errorf("response[%d]: %v", i, err))
			}
		}
		return outErr.ErrorOrNil()
	}
}
