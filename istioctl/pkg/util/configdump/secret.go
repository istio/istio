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
	"encoding/base64"
	"fmt"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	extapi "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

// GetSecretConfigDump retrieves a secret dump from a config dump wrapper
func (w *Wrapper) GetSecretConfigDump() (*admin.SecretsConfigDump, error) {
	secretDumpAny, err := w.getSection(secrets)
	if err != nil {
		return nil, err
	}
	secretDump := &admin.SecretsConfigDump{}
	err = secretDumpAny.UnmarshalTo(secretDump)
	if err != nil {
		return nil, err
	}
	return secretDump, nil
}

// GetRootCAFromSecretConfigDump retrieves root CA from a secret config dump wrapper
func (w *Wrapper) GetRootCAFromSecretConfigDump(anySec *anypb.Any) ([]byte, error) {
	var secret extapi.Secret
	if err := anySec.UnmarshalTo(&secret); err != nil {
		return nil, fmt.Errorf("failed to unmarshall ROOTCA secret: %v", err)
	}
	rCASecret := secret.GetValidationContext()
	if rCASecret != nil {
		trustCA := rCASecret.GetTrustedCa()
		if trustCA != nil {
			inlineBytes := trustCA.GetInlineBytes()
			if inlineBytes != nil {
				rootCA := make([]byte, base64.StdEncoding.DecodedLen(len(inlineBytes)))
				_, err := base64.StdEncoding.Decode(rootCA, inlineBytes)
				if err != nil {
					return nil, fmt.Errorf("failed to decode inlinebytes: %v", err)
				}
				return rootCA, err
			}
			return nil, fmt.Errorf("cannot retrieve inlineBytes from trustCA section")
		}
		return nil, fmt.Errorf("cannot retrieve trustedCa from secret ROOTCA")
	}
	return nil, fmt.Errorf("cannot find ROOTCA from secret config dump")
}
