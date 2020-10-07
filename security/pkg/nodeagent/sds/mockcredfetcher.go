package sds

import (
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/sds/plugin"
)

func NewMockCredFetcher(credtype, trustdomain, jwtPath string) (security.CredFetcher, error) {
	return plugin.CreateMockInteTestPlugin(), nil
}
