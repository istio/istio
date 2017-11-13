package v2

import (
	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
)

type Hasher struct{}

func (Hasher) Hash(node *api.Node) (cache.Key, error) {
	return "service-node", nil
}
