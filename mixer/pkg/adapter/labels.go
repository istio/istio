// Copyright 2017 The Istio Authors.
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

package adapter

import (
	"fmt"

	dpb "istio.io/api/mixer/v1/config/descriptor"
)

// LabelType defines the set of known label types that can be generated
// by the mixer.
type LabelType int

// Supported types of labels.
const (
	String LabelType = iota
	Int64
	Float64
	Bool
	Time
	Duration
	IPAddress
	EmailAddress
	URI
	DNSName
)

// LabelTypeFromProto translates from ValueType to LabelType.
func LabelTypeFromProto(pbt dpb.ValueType) (LabelType, error) {
	switch pbt {
	case dpb.STRING:
		return String, nil
	case dpb.BOOL:
		return Bool, nil
	case dpb.INT64:
		return Int64, nil
	case dpb.DOUBLE:
		return Float64, nil
	case dpb.TIMESTAMP:
		return Time, nil
	case dpb.DNS_NAME:
		return DNSName, nil
	case dpb.DURATION:
		return Duration, nil
	case dpb.EMAIL_ADDRESS:
		return EmailAddress, nil
	case dpb.IP_ADDRESS:
		return IPAddress, nil
	case dpb.URI:
		return URI, nil
	default:
		return 0, fmt.Errorf("unsupported proto ValueType %v", pbt)
	}
}
