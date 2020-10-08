package plugin

import (
	"istio.io/istio/pkg/security"
)

func NewMockCredFetcher(credtype, trustdomain, jwtPath string) (security.CredFetcher, error) {
	return CreateMockInteTestPlugin(), nil
}
