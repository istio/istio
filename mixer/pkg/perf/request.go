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

package perf

import (
	"encoding/json"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	attr "istio.io/istio/mixer/pkg/attribute"
	"istio.io/pkg/attribute"
)

// Request interface is the common interface for all different types of requests.
type Request interface {
	// getRequestProto returns one API request proto.
	getRequestProto() interface{}
}

// BasicReport is an implementation of Request that is used to explicitly specify a Report request.
type BasicReport struct {
	RequestProto istio_mixer_v1.ReportRequest `json:"requestProto,omitempty"`
}

var _ Request = &BasicReport{}

// BasicCheck is an implementation of Request that is specified declaratively by the author.
type BasicCheck struct {
	RequestProto istio_mixer_v1.CheckRequest `json:"requestProto,omitempty"`
}

var _ Request = &BasicCheck{}

// BuildBasicReport builds a BasicReport Request by creating a Report API request.
func BuildBasicReport(attributes map[string]interface{}) BasicReport {
	requestBag := attribute.GetMutableBag(nil)
	for k, v := range attributes {
		switch v := v.(type) {
		case map[string]string:
			requestBag.Set(k, attribute.WrapStringMap(v))
		default:
			requestBag.Set(k, v)
		}
	}

	var attrProto istio_mixer_v1.CompressedAttributes
	attr.ToProto(requestBag, &attrProto, nil, 0)

	br := BasicReport{
		RequestProto: istio_mixer_v1.ReportRequest{
			Attributes: []istio_mixer_v1.CompressedAttributes{attrProto},
		},
	}
	return br
}

func (r BasicReport) getRequestProto() interface{} {
	return interface{}(&r.RequestProto)
}

// MarshalJSON marshals the report as JSON.
func (r BasicReport) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage, 2)

	var err error
	m["type"], _ = json.Marshal("basicReport")
	m["requestProto"], err = json.Marshal(r.RequestProto)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

// BuildBasicCheck builds a BasicReport Request by creating a Check API request.
func BuildBasicCheck(attributes map[string]interface{}, quotas map[string]istio_mixer_v1.CheckRequest_QuotaParams) BasicCheck {
	requestBag := attribute.GetMutableBag(nil)
	for k, v := range attributes {
		switch v := v.(type) {
		case map[string]string:
			requestBag.Set(k, attribute.WrapStringMap(v))
		default:
			requestBag.Set(k, v)
		}
	}

	var attrProto istio_mixer_v1.CompressedAttributes
	attr.ToProto(requestBag, &attrProto, nil, 0)

	c := BasicCheck{
		RequestProto: istio_mixer_v1.CheckRequest{
			Attributes: attrProto,
			Quotas:     quotas,
		},
	}
	return c
}

func (c BasicCheck) getRequestProto() interface{} {
	return interface{}(&c.RequestProto)
}

// MarshalJSON marshals the report as JSON.
func (c BasicCheck) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage, 3)

	var err error
	m["type"], _ = json.Marshal("basicCheck")
	m["requestProto"], err = json.Marshal(c.RequestProto)
	if err != nil {
		return nil, err
	}

	return json.Marshal(m)
}
