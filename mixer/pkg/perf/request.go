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

package perf

import (
	"encoding/json"

	"istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/mock"
)

// Request interface is the common interface for all different types of requests.
type Request interface {
	// createRequestProtos causes the request to create one-or-more API request protos.
	createRequestProtos(c Config) []interface{}
}

// BasicReport is an implementation of Request that is used to explicitly specify a Report request.
type BasicReport struct {
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

var _ Request = &BasicReport{}

// BasicCheck is an implementation of Request that is specified declaratively by the author.
type BasicCheck struct {
	Attributes map[string]interface{}                             `json:"attributes,omitempty"`
	Quotas     map[string]istio_mixer_v1.CheckRequest_QuotaParams `json:"quotas,omitempty"`
}

var _ Request = &BasicCheck{}

// CreateRequest creates a request proto.
func (r BasicReport) createRequestProtos(c Config) []interface{} {
	return []interface{}{
		&istio_mixer_v1.ReportRequest{
			Attributes: []istio_mixer_v1.CompressedAttributes{
				mock.GetAttrBag(r.Attributes,
					c.IdentityAttribute,
					c.IdentityAttributeDomain)},
		},
	}
}

// MarshalJSON marshal the report as JSON.
func (r BasicReport) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)

	var err error
	m["type"], _ = json.Marshal("basicReport")

	m["attributes"], err = json.Marshal(r.Attributes)
	if err != nil {
		return nil, err
	}

	return json.Marshal(m)
}

// CreateRequest creates a request proto.
func (c BasicCheck) createRequestProtos(cfg Config) []interface{} {
	return []interface{}{
		&istio_mixer_v1.CheckRequest{
			Attributes: mock.GetAttrBag(c.Attributes,
				cfg.IdentityAttribute,
				cfg.IdentityAttributeDomain),
			Quotas: c.Quotas,
		},
	}
}

// MarshalJSON marshal the report as JSON.
func (c BasicCheck) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)

	var err error
	m["type"], _ = json.Marshal("basicCheck")

	m["attributes"], err = json.Marshal(c.Attributes)
	if err != nil {
		return nil, err
	}

	if c.Quotas != nil {
		m["quotas"], err = json.Marshal(c.Quotas)
		if err != nil {
			return nil, err
		}
	}

	return json.Marshal(m)
}
