package storage

import (
	"context"

	"github.com/docker/distribution"
	digest "github.com/opencontainers/go-digest"
)

// signedManifestHandler is a ManifestHandler that unmarshals v1 manifests but
// refuses to Put v1 manifests
type v1UnsupportedHandler struct {
	innerHandler ManifestHandler
}

var _ ManifestHandler = &v1UnsupportedHandler{}

func (v *v1UnsupportedHandler) Unmarshal(ctx context.Context, dgst digest.Digest, content []byte) (distribution.Manifest, error) {
	return v.innerHandler.Unmarshal(ctx, dgst, content)
}
func (v *v1UnsupportedHandler) Put(ctx context.Context, manifest distribution.Manifest, skipDependencyVerification bool) (digest.Digest, error) {
	return digest.Digest(""), distribution.ErrSchemaV1Unsupported
}
