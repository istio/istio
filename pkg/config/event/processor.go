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

package event

// Processor is a config event processor.
//
// - Start and Stop can be called multiple times, idempotently.
// - A processor can keep internal state after it is started, but it *must* not carry state over
// between multiple start/stop calls.
// - It must complete all its internal initialization, by the time Start call returns. That is,
// the callers will assume that events can be sent (be be processed) after Start returns.
// - Processor may still receive events after Stop is called. These events must be discarded.
//
type Processor interface {
	Handler

	Start()
	Stop()
}
