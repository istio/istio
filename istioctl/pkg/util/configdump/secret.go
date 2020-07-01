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
	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/ptypes"
)

// GetSecretsConfigDump retrieves a secret dump from a config dump wrapper
func (w *Wrapper) GetSecretConfigDump() (*adminapi.SecretsConfigDump, error) {
	secretDumpAny, err := w.getSection(secrets)
	if err != nil {
		return nil, err
	}
	secretDump := &adminapi.SecretsConfigDump{}
	err = ptypes.UnmarshalAny(secretDumpAny, secretDump)
	if err != nil {
		return nil, err
	}
	return secretDump, nil
}
