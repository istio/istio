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
	"crypto/sha256"
	"encoding/base64"

	"istio.io/pkg/pool"

	"istio.io/istio/pkg/config/resource"
)

// Version synthesizes a new resource version from existing resource versions. There needs to be at least one version
// in versions, otherwise function panics.
func Version(prefix string, versions ...resource.Version) resource.Version {
	i := 0
	return VersionIter(prefix, func() (n resource.FullName, v resource.Version, ok bool) {
		if i < len(versions) {
			v = versions[i]
			i++
			ok = true
		}
		return
	})
}

// VersionIter synthesizes a new resource version from existing resource versions.
func VersionIter(prefix string, iter func() (resource.FullName, resource.Version, bool)) resource.Version {
	b := pool.GetBuffer()

	for {
		n, v, ok := iter()
		if !ok {
			break
		}
		_, _ = b.WriteString(n.String())
		_, _ = b.WriteString(string(v))
	}

	if b.Len() == 0 {
		panic("synthesize.VersionIter: at least one version is required")
	}

	sgn := sha256.Sum256(b.Bytes())

	pool.PutBuffer(b)

	return resource.Version("$" + prefix + "_" + base64.RawStdEncoding.EncodeToString(sgn[:]))
}
