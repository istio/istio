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

package configdump

import (
	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
)

// GetEcdsConfigDump retrieves the extension config dump from the ConfigDump
func (w *Wrapper) GetEcdsConfigDump() (*admin.EcdsConfigDump, error) {
	ecdsDumpAny, err := w.getSections(ecds)
	if err != nil {
		return nil, err
	}

	ecdsDump := &admin.EcdsConfigDump{}
	for _, dump := range ecdsDumpAny {
		ecds := &admin.EcdsConfigDump{}
		err = dump.UnmarshalTo(ecds)
		if err != nil {
			return nil, err
		}

		ecdsDump.EcdsFilters = append(ecdsDump.EcdsFilters, ecds.EcdsFilters...)
	}

	if err != nil {
		return nil, err
	}
	return ecdsDump, nil
}
