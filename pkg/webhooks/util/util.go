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

package util

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"istio.io/istio/pilot/pkg/keycertbundle"
)

type ConfigError struct {
	err    error
	reason string
}

func (e ConfigError) Error() string {
	return e.err.Error()
}

func (e ConfigError) Reason() string {
	return e.reason
}

func LoadCABundle(caBundleWatcher *keycertbundle.Watcher) ([]byte, error) {
	caBundle := caBundleWatcher.GetCABundle()
	if err := VerifyCABundle(caBundle); err != nil {
		return nil, &ConfigError{err, "could not verify caBundle"}
	}

	return caBundle, nil
}

func VerifyCABundle(caBundle []byte) error {
	block, _ := pem.Decode(caBundle)
	if block == nil {
		return errors.New("could not decode pem")
	}
	if block.Type != "CERTIFICATE" {
		return fmt.Errorf("cert contains wrong pem type: %q", block.Type)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("cert contains invalid x509 certificate: %v", err)
	}
	return nil
}
