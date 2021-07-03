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
	"compress/gzip"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	wasmPluginFileName = "plugin.wasm"
)

type ImageFetcherOption struct {
	Username string
	Password string
	// TODO(mathetake) Add signature verification stuff.
}

func (o *ImageFetcherOption) useDefaultKeyChain() bool {
	return o.Username == "" || o.Password == ""
}

type ImageFetcher struct {
	fetchOpt remote.Option
}

func NewImageFetcher(opt ImageFetcherOption) *ImageFetcher {
	var fetchOpt remote.Option
	// TODO(mathetake): have "Anonymous" option?
	if opt.useDefaultKeyChain() {
		// Note that default key chain reads the docker config from DOCKER_CONFIG
		// so must set the envvar when reaching this branch is expected.
		fetchOpt = remote.WithAuthFromKeychain(authn.DefaultKeychain)
	} else {
		fetchOpt = remote.WithAuth(&authn.Basic{Username: opt.Username})
	}
	return &ImageFetcher{
		fetchOpt: fetchOpt,
	}
}

// Fetch is the entrypoint after variant detection the image's manifest annotations.
func (o *ImageFetcher) Fetch(url string) ([]byte, error) {
	ref, err := name.ParseReference(url)
	if err != nil {
		return nil, fmt.Errorf("could not parse url in image reference: %v", err)
	}

	img, err := remote.Image(ref, o.fetchOpt)
	if err != nil {
		return nil, fmt.Errorf("could not fetch image: %v", err)
	}

	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("could not retreive manifest: %v", err)
	}

	if manifest.MediaType == types.DockerManifestSchema2 {
		// This case, assume we have docker images with "application/vnd.docker.distribution.manifest.v2+json"
		// as the manifest media type. Note that the media type of manifest is Docker specific and
		// all OCI images would have an empty string in .MediaType field.
		return extractDockerImage(img)
	}

	// We try to parse it as the "compat" variant image with a single "application/vnd.oci.image.layer.v1.tar+gzip" layer.
	if ret, err := extractOCIStandardImage(img); err == nil {
		return ret, nil
	}

	// Otherwise, we try to parse it as the *oci* variant image with custom artifact media types.
	return extractOCIArtifactImage(img)
}

func extractDockerImage(img v1.Image) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must be single-layered.
	if len(layers) != 1 {
		return nil, fmt.Errorf("number of layers must be 1 but got %d", len(layers))
	}

	layer := layers[0]
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

func extractOCIStandardImage(img v1.Image) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must be single-layered.
	if len(layers) != 1 {
		return nil, fmt.Errorf("number of layers must be 1 but got %d", len(layers))
	}

	layer := layers[0]
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

// Extracts the Wasm plugin binary named "plugin.wasm" in a given tar.gz reader.
func extractWasmPluginBinary(r io.Reader) ([]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layer as tar.gz: %v", err)
	}

	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		ret := make([]byte, h.Size)
		if h.Name == wasmPluginFileName {
			_, err := io.ReadFull(tr, ret)
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %v", wasmPluginFileName, err)
			}
			return ret, nil
		}
	}
	return nil, fmt.Errorf("%s not found in the archive", wasmPluginFileName)
}

func extractOCIArtifactImage(img v1.Image) ([]byte, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must be two-layered.
	if len(layers) != 2 {
		return nil, fmt.Errorf("number of layers must be 2 but got %d", len(layers))
	}

	const wasmLayerMediaType = "application/vnd.module.wasm.content.layer.v1+wasm"
	var layer v1.Layer
	for _, l := range layers {
		mt, err := l.MediaType()
		if err != nil {
			return nil, fmt.Errorf("could not retreive the media type: %v", err)
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
