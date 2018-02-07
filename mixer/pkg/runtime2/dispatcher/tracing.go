// Copyright 2018 Istio Authors
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

package dispatcher

import (
	opentracing "github.com/opentracing/opentracing-go"
	tracelog "github.com/opentracing/opentracing-go/log"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/pkg/status"
)

const (
	meshFunction = "meshFunction"
	handlerName  = "handler"
	adapterName  = "adapter"
	responseCode = "response_code"
	responseMsg  = "response_message"
	errorStr     = "error"
)

// LogToDispatchSpan logs to the given Span in a structured manner. Span must be valid.
func logToDispatchSpan(span opentracing.Span, template string, handler string, adapter string, err error) {
	st := status.OK
	if err != nil {
		st = status.WithError(err)
	}

	span.LogFields(
		tracelog.String(meshFunction, template),
		tracelog.String(handlerName, handler),
		tracelog.String(adapterName, adapter),
		tracelog.String(responseCode, rpc.Code_name[st.Code]),
		tracelog.String(responseMsg, st.Message),
		tracelog.Bool(errorStr, err != nil),
	)
}
