// Copyright 2017 Istio Authors
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

// Functions related to header configuration in envoy: match conditions

package envoy

import (
	"fmt"
	"sort"

	"github.com/golang/glog"

	proxyconfig "istio.io/api/proxy/v1/config"
)

// TODO: test sorting, translation

// buildHeaders skips over URI as it has special meaning
func buildHeaders(matches map[string]*proxyconfig.StringMatch) []Header {
	headers := make([]Header, 0, len(matches))
	for name, match := range matches {
		if name != HeaderURI {
			headers = append(headers, buildHeader(name, match))
		}
	}
	sort.Sort(Headers(headers))
	return headers
}

func buildHeader(name string, match *proxyconfig.StringMatch) Header {
	value := ""
	regex := false

	switch m := match.MatchType.(type) {
	case *proxyconfig.StringMatch_Exact:
		value = m.Exact
	case *proxyconfig.StringMatch_Prefix:
		// TODO(rshriram): escape prefix string into regex, define regex standard
		value = fmt.Sprintf("^%v.*", m.Prefix)
		regex = true
	case *proxyconfig.StringMatch_Regex:
		value = m.Regex
		regex = true
	default:
		glog.Warningf("Missing header match type, defaulting to empty value: %#v", match.MatchType)
	}

	return Header{
		Name:  name,
		Value: value,
		Regex: regex,
	}
}
