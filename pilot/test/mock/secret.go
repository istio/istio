package mock

// SecretRegistry is a mock of the secret registry
type SecretRegistry struct {
	Secrets map[string]map[string][]byte
}

// GetSecret retrieves a secret for the given URI
func (s *SecretRegistry) GetSecret(uri string) (map[string][]byte, error) {
	return s.Secrets[uri], nil
}
