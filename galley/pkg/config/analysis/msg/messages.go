// Copyright 2019 Istio Authors
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

package msg

import (
	. "istio.io/istio/galley/pkg/config/analysis/diag"
)

var (
	// First 20 messages are reserved for generic & internal messages


	// IST0001 is an internal error. Used for internal, fatal errors.
	IST0001 = NewTemplate(Error, Code(1), "Internal error: %v")

	// IST0002 is a generic message for not-yet-implemented features.
	IST0002 = NewTemplate(Error, Code(2), "Not yet implemented: %s")

	// IST0003 is a generic parser error for configuration.
	IST0003 = NewTemplate(Warning, Code(3), "Parse error: %s")

	// IST0004 is a generic message for deprecated features.
	IST0004 = NewTemplate(Warning, Code(4), "Deprecated: %s")

	// Beyond 20 is for analysis failures.


	// IST0020 is an error from VirtualService indicating that the Gateway referenced by the Virtual Service is not found.
	IST0020 = NewTemplate(Error, Code(5), "Referenced Gateway not found: %q")
)
