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

package servicecontrol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	pbtypes "github.com/gogo/protobuf/types"
	rpc "github.com/googleapis/googleapis/google/rpc"
)

const (
	apiKeyPrefix = "api_key:"

	logDebug = 4
)

func toRPCCode(responseCode int) rpc.Code {
	switch responseCode {
	case 200:
		return rpc.OK
	case 401:
		return rpc.UNAUTHENTICATED
	case 400:
		return rpc.INVALID_ARGUMENT
	case 403:
		return rpc.PERMISSION_DENIED
	case 404:
		return rpc.NOT_FOUND
	case 409:
		return rpc.ALREADY_EXISTS
	case 429:
		return rpc.RESOURCE_EXHAUSTED
	case 499:
		return rpc.CANCELLED
	case 500:
		return rpc.INTERNAL
	case 501:
		return rpc.UNIMPLEMENTED
	case 503:
		return rpc.UNAVAILABLE
	case 504:
		return rpc.DEADLINE_EXCEEDED
	default:
		if responseCode >= 200 && responseCode <= 300 {
			return rpc.OK
		}
		if responseCode >= 400 && responseCode <= 500 {
			return rpc.FAILED_PRECONDITION
		}
	}
	return rpc.UNKNOWN
}

func serviceControlErrorToRPCCode(errorCode string) rpc.Code {
	switch errorCode {
	case "NOT_FOUND":
		return rpc.NOT_FOUND
	case "PERMISSION_DENIED",
		"SECURITY_POLICY_VIOLATED":
		return rpc.PERMISSION_DENIED
	case "RESOURCE_EXHAUSTED",
		"BUDGET_EXCEEDED",
		"LOAD_SHEDDING",
		"ABUSER_DETECTED":
		return rpc.RESOURCE_EXHAUSTED
	case "SERVICE_NOT_ACTIVATED",
		"VISIBILITY_DENIED",
		"BILLING_DISABLED",
		"PROJECT_DELETED",
		"PROJECT_INVALID",
		"IP_ADDRESS_BLOCKED",
		"REFERER_BLOCKED",
		"CLIENT_APP_BLOCKED",
		"API_TARGET_BLOCKED",
		"LOAS_PROJECT_DISABLED",
		"SERVICE_STATUS_UNAVAILABLE",
		"BILLING_STATUS_UNAVAILABLE",
		"QUOTA_CHECK_UNAVAILABLE",
		"LOAS_PROJECT_LOOKUP_UNAVAILABLE",
		"CLOUD_RESOURCE_MANAGER_BACKEND_UNAVAILABLE",
		"SECURITY_POLICY_BACKEND_UNAVAILABLE":
		return rpc.UNAVAILABLE
	case "API_KEY_INVALID",
		"API_KEY_EXPIRED",
		"API_KEY_NOT_FOUND",
		"SPATULA_HEADER_INVALID":
		return rpc.INVALID_ARGUMENT
	}
	return rpc.UNKNOWN
}

// Resolve interface{} to a string value. Return "", false if value isn't a string
func resolveToString(value interface{}) (string, bool) {
	if value == nil {
		return "", false
	}
	result, ok := value.(string)
	return result, ok
}

func toDuration(durationProto *pbtypes.Duration) time.Duration {
	duration, err := pbtypes.DurationFromProto(durationProto)
	if err != nil {
		panic(fmt.Sprintf("invalid Duration proto: %v", err))
	}
	return duration
}

func getInt64Address(i int64) *int64 {
	addr := new(int64)
	*addr = i
	return addr
}

func generateConsumerIDFromAPIKey(apiKey string) string {
	return apiKeyPrefix + apiKey
}

func dimensionToString(dimensions map[string]interface{}, key string) (string, bool) {
	if value, ok := resolveToString(dimensions[key]); ok {
		return value, true
	}
	return "", false
}

func toFormattedJSON(marshaller json.Marshaler) (string, error) {
	value, err := marshaller.MarshalJSON()
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	err = json.Indent(&out, value, "", " ")
	if err != nil {
		return "", err
	}
	return string(out.Bytes()), err
}
