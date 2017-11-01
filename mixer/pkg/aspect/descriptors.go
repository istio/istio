// Copyright 2016 Istio Authors
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

package aspect

import (
	"fmt"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/adapter"
)

// valueTypeToLabelType translates from ValueType to LabelType.
func valueTypeToLabelType(pbt dpb.ValueType) (adapter.LabelType, error) {
	switch pbt {
	case dpb.STRING:
		return adapter.String, nil
	case dpb.BOOL:
		return adapter.Bool, nil
	case dpb.INT64:
		return adapter.Int64, nil
	case dpb.DOUBLE:
		return adapter.Float64, nil
	case dpb.TIMESTAMP:
		return adapter.Time, nil
	case dpb.DNS_NAME:
		return adapter.DNSName, nil
	case dpb.DURATION:
		return adapter.Duration, nil
	case dpb.EMAIL_ADDRESS:
		return adapter.EmailAddress, nil
	case dpb.IP_ADDRESS:
		return adapter.IPAddress, nil
	case dpb.URI:
		return adapter.URI, nil
	case dpb.STRING_MAP:
		return adapter.StringMap, nil
	default:
		return 0, fmt.Errorf("unsupported proto ValueType %v", pbt)
	}
}
