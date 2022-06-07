// Copyright Istio Authors.
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

package multixds

import (
	"fmt"
	"net/url"
	"strings"
)

func isMCPAddr(u *url.URL) bool {
	return strings.HasSuffix(u.Host, ".googleapis.com") || strings.HasSuffix(u.Host, ".googleapis.com:443")
}

func parseMCPAddr(u *url.URL) (*xdsAddr, error) {
	ret := &xdsAddr{host: u.Host}
	if !strings.HasSuffix(ret.host, ":443") {
		ret.host += ":443"
	}
	const projSeg = "/projects/"
	i := strings.Index(u.Path, projSeg)
	if i == -1 {
		return nil, fmt.Errorf("webhook URL %s doesn't contain the projects segment", u)
	}
	i += len(projSeg)
	j := strings.IndexByte(u.Path[i:], '/')
	if j == -1 {
		return nil, fmt.Errorf("webhook URL %s is malformed", u)
	}
	ret.gcpProject = u.Path[i : i+j]

	const crSeg = "/ISTIO_META_CLOUDRUN_ADDR/"
	i += j
	j = strings.Index(u.Path[i:], crSeg)
	if j == -1 {
		return nil, fmt.Errorf("webhook URL %s is missing %s", u, crSeg)
	}
	ret.istiod = u.Path[i+j+len(crSeg):]

	return ret, nil
}
