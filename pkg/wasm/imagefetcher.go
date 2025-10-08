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

package wasm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/config/configfile"
	dtypes "github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/hashicorp/go-multierror"
)

// This file implements the fetcher of "Wasm Image Specification" compatible container images.
// The spec is here https://github.com/solo-io/wasm/blob/master/spec/README.md.
// Basically, this supports fetching and unpackaging three types of container images containing a Wasm binary.

type ImageFetcherOption struct {
	// TODO(mathetake) Add signature verification stuff.
	PullSecret []byte
	Insecure   bool
}

func (o *ImageFetcherOption) useDefaultKeyChain() bool {
	return o.PullSecret == nil
}

func (o ImageFetcherOption) String() string {
	if o.PullSecret == nil {
		return fmt.Sprintf("{Insecure: %v}", o.Insecure)
	}
	return fmt.Sprintf("{Insecure: %v, PullSecret: <redacted>}", o.Insecure)
}

type ImageFetcher struct {
	fetchOpts []remote.Option
}

func NewImageFetcher(ctx context.Context, opt ImageFetcherOption) *ImageFetcher {
	fetchOpts := make([]remote.Option, 0, 2)
	// TODO(mathetake): have "Anonymous" option?
	if opt.useDefaultKeyChain() {
		// Note that default key chain reads the docker config from DOCKER_CONFIG
		// so must set the envvar when reaching this branch is expected.
		fetchOpts = append(fetchOpts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	} else {
		fetchOpts = append(fetchOpts, remote.WithAuthFromKeychain(&wasmKeyChain{data: opt.PullSecret}))
	}

	if opt.Insecure {
		t := remote.DefaultTransport.(*http.Transport).Clone()
		// nolint: gosec
		// This is only when a user explicitly sets a flag to enable insecure mode
		t.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: opt.Insecure,
		}
		fetchOpts = append(fetchOpts, remote.WithTransport(t))
	}

	return &ImageFetcher{
		fetchOpts: append(fetchOpts, remote.WithContext(ctx)),
	}
}

// PrepareFetch is the entrypoint for fetching Wasm binary from Wasm Image Specification compatible images.
// Wasm binary is not fetched immediately, but returned by `binaryFetcher` function, which is returned by PrepareFetch.
// By this way, we can have another chance to check cache with `actualDigest` without downloading the OCI image.
func (o *ImageFetcher) PrepareFetch(url string) (binaryFetcher func() ([]byte, error), actualDigest string, err error) {
	ref, err := name.ParseReference(url)
	if err != nil {
		err = fmt.Errorf("could not parse url in image reference: %v", err)
		return binaryFetcher, actualDigest, err
	}
	wasmLog.Infof("fetching image %s from registry %s with tag %s", ref.Context().RepositoryStr(),
		ref.Context().RegistryStr(), ref.Identifier())

	// fallback to http based request, inspired by [helm](https://github.com/helm/helm/blob/12f1bc0acdeb675a8c50a78462ed3917fb7b2e37/pkg/registry/client.go#L594)
	// only deal with https fallback instead of attributing all other type of errors to URL parsing error
	desc, err := remote.Get(ref, o.fetchOpts...)
	if err != nil && strings.Contains(err.Error(), "server gave HTTP response") {
		wasmLog.Infof("fetching image with plain text from %s", url)
		ref, err = name.ParseReference(url, name.Insecure)
		if err == nil {
			desc, err = remote.Get(ref, o.fetchOpts...)
		}
	}

	if err != nil {
		err = fmt.Errorf("could not fetch manifest: %v", err)
		return binaryFetcher, actualDigest, err
	}

	// Fetch image.
	img, err := desc.Image()
	if err != nil {
		err = fmt.Errorf("could not fetch image: %v", err)
		return binaryFetcher, actualDigest, err
	}

	// Check Manifest's digest if expManifestDigest is not empty.
	d, _ := img.Digest()
	actualDigest = d.Hex
	binaryFetcher = func() ([]byte, error) {
		manifest, err := img.Manifest()
		if err != nil {
			return nil, fmt.Errorf("could not retrieve manifest: %v", err)
		}

		if manifest.MediaType == types.DockerManifestSchema2 {
			// This case, assume we have docker images with "application/vnd.docker.distribution.manifest.v2+json"
			// as the manifest media type. Note that the media type of manifest is Docker specific and
			// all OCI images would have an empty string in .MediaType field.
			ret, err := extractDockerImage(img)
			if err != nil {
				return nil, fmt.Errorf("could not extract Wasm file from the image as Docker container %v", err)
			}
			return ret, nil
		}

		// We try to parse it as the "compat" variant image with a single "application/vnd.oci.image.layer.v1.tar+gzip" layer.
		ret, errCompat := extractOCIStandardImage(img)
		if errCompat == nil {
			return ret, nil
		}

		// Otherwise, we try to parse it as the *oci* variant image with custom artifact media types.
		ret, errOCI := extractOCIArtifactImage(img)
		if errOCI == nil {
			return ret, nil
		}

		// We failed to parse the image in any format, so wrap the errors and return.
		return nil, fmt.Errorf("the given image is in invalid format as an OCI image: %v",
			multierror.Append(err,
				fmt.Errorf("could not parse as compat variant: %v", errCompat),
				fmt.Errorf("could not parse as oci variant: %v", errOCI),
			),
		)
	}
	return binaryFetcher, actualDigest, err
}

// extractDockerImage extracts the Wasm binary from the
// *compat* variant Wasm image with the standard Docker media type: application/vnd.docker.image.rootfs.diff.tar.gzip.
// https://github.com/solo-io/wasm/blob/master/spec/spec-compat.md#specification
func extractDockerImage(img v1.Image) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must have at least one layer.
	if len(layers) == 0 {
		return nil, errors.New("number of layers must be greater than zero")
	}

	layer := layers[len(layers)-1]
	mt, err := layer.MediaType()
	if err != nil {
		return nil, fmt.Errorf("could not get media type: %v", err)
	}

	// Media type must be application/vnd.docker.image.rootfs.diff.tar.gzip.
	if mt != types.DockerLayer {
		return nil, fmt.Errorf("invalid media type %s (expect %s)", mt, types.DockerLayer)
	}

	r, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("could not get layer content: %v", err)
	}
	defer r.Close()

	ret, err := extractWasmPluginBinary(r)
	if err != nil {
		return nil, fmt.Errorf("could not extract wasm binary: %v", err)
	}
	return ret, nil
}

// extractOCIStandardImage extracts the Wasm binary from the
// *compat* variant Wasm image with the standard OCI media type: application/vnd.oci.image.layer.v1.tar+gzip.
// https://github.com/solo-io/wasm/blob/master/spec/spec-compat.md#specification
func extractOCIStandardImage(img v1.Image) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must have at least one layer.
	if len(layers) == 0 {
		return nil, fmt.Errorf("number of layers must be greater than zero")
	}

	layer := layers[len(layers)-1]
	mt, err := layer.MediaType()
	if err != nil {
		return nil, fmt.Errorf("could not get media type: %v", err)
	}

	// Check if the layer is "application/vnd.oci.image.layer.v1.tar+gzip".
	if types.OCILayer != mt {
		return nil, fmt.Errorf("invalid media type %s (expect %s)", mt, types.OCILayer)
	}

	r, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("could not get layer content: %v", err)
	}
	defer r.Close()

	ret, err := extractWasmPluginBinary(r)
	if err != nil {
		return nil, fmt.Errorf("could not extract wasm binary: %v", err)
	}
	return ret, nil
}

// Extracts the Wasm plugin binary named "plugin.wasm" in a given reader for tar.gz.
// This is only used for *compat* variant.
func extractWasmPluginBinary(r io.Reader) ([]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layer as tar.gz: %v", err)
	}

	// The target file name for Wasm binary.
	// https://github.com/solo-io/wasm/blob/master/spec/spec-compat.md#specification
	const wasmPluginFileName = "plugin.wasm"

	// Search for the file walking through the archive.

	// Limit wasm binary to 256MB; in reality it must be much smaller
	tr := tar.NewReader(io.LimitReader(gr, 1024*1024*256))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		ret := make([]byte, h.Size)
		if filepath.Base(h.Name) == wasmPluginFileName {
			_, err := io.ReadFull(tr, ret)
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %v", wasmPluginFileName, err)
			}
			return ret, nil
		}
	}
	return nil, fmt.Errorf("%s not found in the archive", wasmPluginFileName)
}

// extractOCIArtifactImage extracts the Wasm binary from the
// *oci* variant Wasm image: https://github.com/solo-io/wasm/blob/master/spec/spec.md#format
func extractOCIArtifactImage(img v1.Image) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must be two-layered.
	if len(layers) != 2 {
		return nil, fmt.Errorf("number of layers must be 2 but got %d", len(layers))
	}

	// The layer type of the Wasm binary itself in *oci* variant.
	const wasmLayerMediaType = "application/vnd.module.wasm.content.layer.v1+wasm"

	// Find the target layer walking through the layers.
	var layer v1.Layer
	for _, l := range layers {
		mt, err := l.MediaType()
		if err != nil {
			return nil, fmt.Errorf("could not retrieve the media type: %v", err)
		}
		if mt == wasmLayerMediaType {
			layer = l
			break
		}
	}

	if layer == nil {
		return nil, fmt.Errorf("could not find the layer of type %s", wasmLayerMediaType)
	}

	// Somehow go-containerregistry recognizes custom artifact layers as compressed ones,
	// while the Solo's Wasm layer is actually uncompressed and therefore
	// the content itself is a raw Wasm binary. So using "Uncompressed()" here result in errors
	// since internally it tries to umcompress it as gzipped blob.
	r, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("could not get layer content: %v", err)
	}
	defer r.Close()

	// Just read it since the content is already a raw Wasm binary as mentioned above.
	ret, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("could not extract wasm binary: %v", err)
	}
	return ret, nil
}

type wasmKeyChain struct {
	data []byte
}

// Resolve an image reference to a credential.
// The function code is borrowed from https://github.com/google/go-containerregistry/blob/v0.8.0/pkg/authn/keychain.go#L65,
// by making it take dockerconfigjson directly as bytes instead of reading from files.
func (k *wasmKeyChain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	if bytes.Equal(k.data, []byte("null")) {
		// Filter out key chain with content "null" to prevent crash at underlying docker library.
		// Remove this check when https://github.com/docker/cli/pull/3434 is merged.
		return nil, fmt.Errorf("")
	}
	reader := bytes.NewReader(k.data)
	cf := configfile.ConfigFile{}
	if err := cf.LoadFromReader(reader); err != nil {
		return nil, err
	}
	key := target.RegistryStr()
	if key == name.DefaultRegistry {
		key = authn.DefaultAuthKey
	}
	cfg, err := cf.GetAuthConfig(key)
	if err != nil {
		return nil, err
	}

	empty := dtypes.AuthConfig{}
	if cfg == empty {
		return authn.Anonymous, nil
	}
	authConfig := authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}
	return authn.FromConfig(authConfig), nil
}
