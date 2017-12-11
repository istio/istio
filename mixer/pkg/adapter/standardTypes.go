// Copyright 2016 Istio Authors
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

package adapter

// Strong Go types for template-specific instance fields that are sent to the adapters.
type (
	// DNSName is associated with template field type istio.mixer.v1.template.DNSName
	DNSName string
	// EmailAddress is associated with template field type istio.mixer.v1.template.EmailAddress
	EmailAddress string
	// URI is associated with template field type istio.mixer.v1.template.Uri
	URI string

	// For other types in "istio.mixer.v1.template", we use well known Go types. For example:
	// istio.mixer.v1.template.Duration -> time.Duration
	// istio.mixer.v1.template.TimeStamp -> time.time
	// istio.mixer.v1.template.IPAddress -> net.IP
)
