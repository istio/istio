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

package svcctrl

import (
	"testing"
	"time"

	"encoding/json"

	pbtypes "github.com/gogo/protobuf/types"
	rpc "github.com/googleapis/googleapis/google/rpc"
	sc "google.golang.org/api/servicecontrol/v1"
)

type testMarshaller struct{}

func (t *testMarshaller) MarshalJSON() ([]byte, error) {
	data := struct {
		Field string `json:"F"`
	}{
		Field: "a",
	}
	return json.Marshal(data)
}

func TestToRpcCode(t *testing.T) {
	httpRPCCodeMap := map[int]rpc.Code{
		200: rpc.OK,
		400: rpc.INVALID_ARGUMENT,
		401: rpc.UNAUTHENTICATED,
		403: rpc.PERMISSION_DENIED,
		404: rpc.NOT_FOUND,
		409: rpc.ALREADY_EXISTS,
		499: rpc.CANCELLED,
		500: rpc.INTERNAL,
		501: rpc.UNIMPLEMENTED,
		503: rpc.UNAVAILABLE,
		504: rpc.DEADLINE_EXCEEDED,
		300: rpc.OK,
		410: rpc.FAILED_PRECONDITION,
		900: rpc.UNKNOWN,
	}
	for httpCode, rpcCode := range httpRPCCodeMap {
		if toRPCCode(httpCode) != rpcCode {
			t.Errorf(`toRPCCode(%d) != %v`, httpCode, rpcCode)
		}
	}
}

func TestCheckErrorToRpcCode(t *testing.T) {
	checkErrorRPCCodeMap := map[rpc.Code][]string{
		rpc.NOT_FOUND:         {"NOT_FOUND"},
		rpc.PERMISSION_DENIED: {"PERMISSION_DENIED", "SECURITY_POLICY_VIOLATED"},
		rpc.RESOURCE_EXHAUSTED: {
			"RESOURCE_EXHAUSTED",
			"BUDGET_EXCEEDED",
			"LOAD_SHEDDING",
			"ABUSER_DETECTED",
		},
		rpc.UNAVAILABLE: {
			"SERVICE_NOT_ACTIVATED",
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
			"SECURITY_POLICY_BACKEND_UNAVAILABLE",
		},
		rpc.INVALID_ARGUMENT: {
			"API_KEY_INVALID",
			"API_KEY_EXPIRED",
			"API_KEY_NOT_FOUND",
			"SPATULA_HEADER_INVALID",
		},
		rpc.UNKNOWN: {"UNKNOWN"},
	}

	for rpcCode, checkErrs := range checkErrorRPCCodeMap {
		for _, err := range checkErrs {
			checkError := &sc.CheckError{
				Code: err,
			}
			if serviceControlErrorToRPCCode(checkError.Code) != rpcCode {
				t.Errorf(`serviceControlErrorToRPCCode(%v) != %v`, checkError, rpcCode)
			}
		}
	}
}

func TestToDuration(t *testing.T) {
	expected := time.Second + time.Nanosecond
	protoDuraiton := pbtypes.DurationProto(expected)
	convertedDuration := toDuration(protoDuraiton)

	if convertedDuration != expected {
		t.Errorf(`expected time.Duration %v, but get %v`, expected, convertedDuration)
	}
}

func TestGetInt64Address(t *testing.T) {
	addr := getInt64Address(123)
	if addr == nil {
		t.Fatalf(`getInt64Address(123) returns nil`)
	}

	if *addr != 123 {
		t.Errorf(`expect *getInt64Addr(123) == 123, but get %v`, *addr)
	}
}

func TestGenerateConsumerIDFromAPIKey(t *testing.T) {
	id := generateConsumerIDFromAPIKey("test-key")
	if id != "api_key:test-key" {
		t.Errorf(` generateConsumerIDFromAPIKey("test-key") returns %v`, id)
	}
}

func TestToFormattedJSON(t *testing.T) {
	formattedJSON, err := toFormattedJSON(&testMarshaller{})
	if err != nil {
		t.Fatalf(`toFormattedJSON failed with %v`, err)
	}

	expected := "{\x0A \"F\": \"a\"\x0A}"

	if formattedJSON != expected {
		t.Errorf(`expected formatted JSON, expect '%s', but get '%s'`, expected, formattedJSON)
	}
}
