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
	"encoding/json"
	"errors"
	"reflect"

	sc "google.golang.org/api/servicecontrol/v1"
)

type mockSvcctrlClient struct {
	serviceName           string
	checkRequest          *sc.CheckRequest
	checkResponse         *sc.CheckResponse
	reportRequest         *sc.ReportRequest
	reportResponse        *sc.ReportResponse
	allocateQuotaRequest  *sc.AllocateQuotaRequest
	allocateQuotaResponse *sc.AllocateQuotaResponse
}

func (c *mockSvcctrlClient) Check(serviceName string, request *sc.CheckRequest) (*sc.CheckResponse, error) {
	c.serviceName = serviceName
	c.checkRequest = request
	if c.checkResponse != nil {
		return c.checkResponse, nil
	}
	return nil, errors.New("injected error")
}

func (c *mockSvcctrlClient) Report(serviceName string, request *sc.ReportRequest) (*sc.ReportResponse, error) {
	c.serviceName = serviceName
	c.reportRequest = request
	if c.reportResponse != nil {
		return c.reportResponse, nil
	}
	return nil, errors.New("injected error")
}

func (c *mockSvcctrlClient) AllocateQuota(serviceName string,
	request *sc.AllocateQuotaRequest) (*sc.AllocateQuotaResponse, error) {
	c.serviceName = serviceName
	c.allocateQuotaRequest = request
	if c.allocateQuotaResponse != nil {
		return c.allocateQuotaResponse, nil
	}
	return nil, errors.New("injected error")
}

func (c *mockSvcctrlClient) setCheckResponse(response *sc.CheckResponse) {
	c.checkResponse = response
}

func (c *mockSvcctrlClient) setReportResponse(response *sc.ReportResponse) {
	c.reportResponse = response
}

func (c *mockSvcctrlClient) setQuotaAllocateRespone(response *sc.AllocateQuotaResponse) {
	c.allocateQuotaResponse = response
}

func (c *mockSvcctrlClient) reset() {
	*c = mockSvcctrlClient{}
}

// Work around linter bug with test code
type mockConsumerProjectIDResolver struct {
	consumerProjectID string
}

func (r *mockConsumerProjectIDResolver) ResolveConsumerProjectID(
	rawAPIKey, OpName string) (string, error) {
	if r.consumerProjectID == "" {
		return "", errors.New("injected error")
	}
	return r.consumerProjectID, nil
}

// Work around linter bug with test code
func compareJSON(s1, s2 string) bool {
	var o1 interface{}
	var o2 interface{}

	if err := json.Unmarshal([]byte(s1), &o1); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(s2), &o2); err != nil {
		return false
	}

	return reflect.DeepEqual(o1, o2)
}
