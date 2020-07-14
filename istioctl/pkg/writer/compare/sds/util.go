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

package sdscompare

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	envoy_admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/istioctl/pkg/util/configdump"
	"istio.io/pkg/log"
)

// SecretItemDiff represents a secret that has been diffed between nodeagent and proxy
type SecretItemDiff struct {
	Agent string `json:"agent"`
	Proxy string `json:"proxy"`
	SecretItem
}

// SecretItem is an intermediate representation of secrets, used to provide a common
// format between the envoy proxy secrets and node agent output which can be diffed
type SecretItem struct {
	Name        string `json:"resource_name"`
	Data        string `json:"cert"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	State       string `json:"state"`
	SecretMeta
}

// SecretMeta holds selected fields which can be extracted from parsed x509 cert
type SecretMeta struct {
	Valid        bool   `json:"cert_valid"`
	SerialNumber string `json:"serial_number"`
	NotAfter     string `json:"not_after"`
	NotBefore    string `json:"not_before"`
	Type         string `json:"type"`
}

// NewSecretItemBuilder returns a new builder to create a secret item
func NewSecretItemBuilder() SecretItemBuilder {
	return &secretItemBuilder{}
}

// SecretItemBuilder wraps the process of setting fields for the SecretItem
// and builds the Metadata fields from the cert contents behind the scenes
type SecretItemBuilder interface {
	Name(string) SecretItemBuilder
	Data(string) SecretItemBuilder
	Source(string) SecretItemBuilder
	Destination(string) SecretItemBuilder
	State(string) SecretItemBuilder
	Build() (SecretItem, error)
}

// secretItemBuilder implements SecretItemBuilder, and acts as an intermediate before SecretItem generation
type secretItemBuilder struct {
	name   string
	data   string
	source string
	dest   string
	state  string
	SecretMeta
}

// Name sets the name field on a secretItemBuilder
func (s *secretItemBuilder) Name(name string) SecretItemBuilder {
	s.name = name
	return s
}

// Data sets the data field on a secretItemBuilder
func (s *secretItemBuilder) Data(data string) SecretItemBuilder {
	s.data = data
	return s
}

// Source sets the source field on a secretItemBuilder
func (s *secretItemBuilder) Source(source string) SecretItemBuilder {
	s.source = source
	return s
}

// Destination sets the destination field on a secretItemBuilder
func (s *secretItemBuilder) Destination(dest string) SecretItemBuilder {
	s.dest = dest
	return s
}

// State sets the state of the secret on the agent or sidecar
func (s *secretItemBuilder) State(state string) SecretItemBuilder {
	s.state = state
	return s
}

// Build takes the set fields from the builder and constructs the actual SecretItem
// including generating the SecretMeta from the supplied cert data, if present
func (s *secretItemBuilder) Build() (SecretItem, error) {
	result := SecretItem{
		Name:        s.name,
		Data:        s.data,
		Source:      s.source,
		Destination: s.dest,
		State:       s.state,
	}

	var meta SecretMeta
	var err error
	if s.data != "" {
		meta, err = secretMetaFromCert([]byte(s.data))
		if err != nil {
			log.Debugf("failed to parse secret resource %s from source %s: %v",
				s.name, s.source, err)
			result.Valid = false
			return result, nil
		}
		result.SecretMeta = meta
		result.Valid = true
		return result, nil
	}
	result.Valid = false
	return result, nil
}

// GetEnvoySecrets parses the secrets section of the config dump into []SecretItem
func GetEnvoySecrets(
	wrapper *configdump.Wrapper) ([]SecretItem, error) {
	secretConfigDump, err := wrapper.GetSecretConfigDump()
	if err != nil {
		return nil, err
	}

	proxySecretItems := make([]SecretItem, 0)
	for _, warmingSecret := range secretConfigDump.DynamicWarmingSecrets {
		secret, err := parseDynamicSecret(warmingSecret, "WARMING")
		if err != nil {
			return nil, fmt.Errorf("failed building warming secret %s: %v",
				warmingSecret.Name, err)
		}
		proxySecretItems = append(proxySecretItems, secret)
	}
	for _, activeSecret := range secretConfigDump.DynamicActiveSecrets {
		secret, err := parseDynamicSecret(activeSecret, "ACTIVE")
		if err != nil {
			return nil, fmt.Errorf("failed building warming secret %s: %v",
				activeSecret.Name, err)
		}
		proxySecretItems = append(proxySecretItems, secret)
	}
	return proxySecretItems, nil
}

func parseDynamicSecret(s *envoy_admin.SecretsConfigDump_DynamicSecret, state string) (SecretItem, error) {
	builder := NewSecretItemBuilder()
	builder.Name(s.Name).State(state)

	secretTyped := &auth.Secret{}
	err := ptypes.UnmarshalAny(s.GetSecret(), secretTyped)
	if err != nil {
		return SecretItem{}, err
	}

	certChainSecret := secretTyped.
		GetTlsCertificate().
		GetCertificateChain().
		GetInlineBytes()
	caDataSecret := secretTyped.
		GetValidationContext().
		GetTrustedCa().
		GetInlineBytes()

	// seems as though the most straightforward way to tell whether this is a root ca or not
	// is to check whether the inline bytes of the cert chain or the trusted ca field is zero length
	if len(certChainSecret) > 0 {
		builder.Data(string(certChainSecret))
	} else if len(caDataSecret) > 0 {
		builder.Data(string(caDataSecret))
	}

	secret, err := builder.Build()
	if err != nil {
		return SecretItem{}, fmt.Errorf("error building secret: %v", err)
	}

	return secret, nil
}

func secretMetaFromCert(rawCert []byte) (SecretMeta, error) {
	block, _ := pem.Decode(rawCert)
	if block == nil {
		return SecretMeta{}, fmt.Errorf("failed to parse certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return SecretMeta{}, err
	}
	var certType string
	if cert.IsCA {
		certType = "CA"
	} else {
		certType = "Cert Chain"
	}

	return SecretMeta{
		SerialNumber: fmt.Sprintf("%d", cert.SerialNumber),
		NotAfter:     cert.NotAfter.Format(time.RFC3339),
		NotBefore:    cert.NotBefore.Format(time.RFC3339),
		Type:         certType,
	}, nil
}
