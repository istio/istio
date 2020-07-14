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

package bypass

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/google/uuid"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
)

func convertValue(value interface{}) (*v1beta1.Value, error) {
	switch v := value.(type) {
	case string:
		return &v1beta1.Value{
			Value: &v1beta1.Value_StringValue{StringValue: v},
		}, nil

	case int64:
		return &v1beta1.Value{
			Value: &v1beta1.Value_Int64Value{Int64Value: v},
		}, nil

	case float64:
		return &v1beta1.Value{
			Value: &v1beta1.Value_DoubleValue{DoubleValue: v},
		}, nil

	case bool:
		return &v1beta1.Value{
			Value: &v1beta1.Value_BoolValue{BoolValue: v},
		}, nil

	case time.Duration:
		return &v1beta1.Value{
			Value: &v1beta1.Value_DurationValue{DurationValue: &v1beta1.Duration{
				Value: types.DurationProto(v),
			}},
		}, nil

	case time.Time:
		ts, err := types.TimestampProto(v)
		if err != nil {
			return nil, err
		}

		return &v1beta1.Value{
			Value: &v1beta1.Value_TimestampValue{
				TimestampValue: &v1beta1.TimeStamp{Value: ts},
			},
		}, nil

	case adapter.DNSName:
		return &v1beta1.Value{
			Value: &v1beta1.Value_DnsNameValue{
				DnsNameValue: &v1beta1.DNSName{
					Value: string(v),
				},
			},
		}, nil

	case adapter.EmailAddress:
		return &v1beta1.Value{
			Value: &v1beta1.Value_EmailAddressValue{
				EmailAddressValue: &v1beta1.EmailAddress{
					Value: string(v),
				},
			},
		}, nil

	case adapter.URI:
		return &v1beta1.Value{
			Value: &v1beta1.Value_UriValue{
				UriValue: &v1beta1.Uri{
					Value: string(v),
				},
			},
		}, nil

	case []byte:
		return &v1beta1.Value{
			Value: &v1beta1.Value_IpAddressValue{
				IpAddressValue: &v1beta1.IPAddress{
					Value: v,
				},
			},
		}, nil

	default:
		return nil, fmt.Errorf("unrecognized type for bypass conversion: %v", value)
	}
}

func convertMapValue(value map[string]interface{}) (map[string]*v1beta1.Value, error) {
	result := make(map[string]*v1beta1.Value)

	for k, v := range value {
		value, err := convertValue(v)
		if err != nil {
			return nil, err
		}

		result[k] = value
	}

	return result, nil
}

func newDedupID() string {
	return uuid.New().String()
}
