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

package synthesize

import (
	"fmt"

	"istio.io/istio/galley/pkg/config/resource"
)

// Version synthesize a new resource version from existing resource versions. There needs to be at least one version
// in versions, otherwise function panics.
func Version(prefix string, versions ...resource.Version) resource.Version {
	if len(versions) == 0 {
		panic("synthesize.Version: at least one version required")
	}

	raw := fmt.Sprintf("$%s_%s", prefix, string(versions[0]))

	count := len(versions)
	for i := 1; i < count; i++ {
		raw += fmt.Sprintf("_%s", string(versions[i]))
	}

	return resource.Version(raw)
}
