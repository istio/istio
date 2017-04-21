package mock

import (
	"istio.io/manager/model"
)

// SecretRegistry is a mock of the secret registry
type SecretRegistry map[string]*model.TLSSecret

// GetTLSSecret retrieves a secret for the given uri
func (s SecretRegistry) GetTLSSecret(uri string) (*model.TLSSecret, error) {
	return s[uri], nil
}
