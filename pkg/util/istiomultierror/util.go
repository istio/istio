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

package istiomultierror

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
)

// MultiErrorFormat provides a format for multierrors. This matches the default format, but if there
// is only one error we will not expand to multiple lines.
func MultiErrorFormat() multierror.ErrorFormatFunc {
	return func(es []error) string {
		if len(es) == 1 {
			return es[0].Error()
		}

		points := make([]string, len(es))
		for i, err := range es {
			points[i] = fmt.Sprintf("* %s", err)
		}

		return fmt.Sprintf(
			"%d errors occurred:\n\t%s\n\n",
			len(es), strings.Join(points, "\n\t"))
	}
}

func New() *multierror.Error {
	return &multierror.Error{
		ErrorFormat: MultiErrorFormat(),
	}
}
