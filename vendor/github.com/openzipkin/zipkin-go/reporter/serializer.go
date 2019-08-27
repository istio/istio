// Copyright 2019 The OpenZipkin Authors
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

package reporter

import (
	"encoding/json"

	"github.com/openzipkin/zipkin-go/model"
)

// SpanSerializer describes the methods needed for allowing to set Span encoding
// type for the various Zipkin transports.
type SpanSerializer interface {
	Serialize([]*model.SpanModel) ([]byte, error)
	ContentType() string
}

// JSONSerializer implements the default JSON encoding SpanSerializer.
type JSONSerializer struct{}

// Serialize takes an array of Zipkin SpanModel objects and returns a JSON
// encoding of it.
func (JSONSerializer) Serialize(spans []*model.SpanModel) ([]byte, error) {
	return json.Marshal(spans)
}

// ContentType returns the ContentType needed for this encoding.
func (JSONSerializer) ContentType() string {
	return "application/json"
}
