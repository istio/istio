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
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type OCIIMageFetcherOption struct {
	Username string
	Password string
	// TODO(mathetake) Add signature verification stuff.
}

func (o *OCIIMageFetcherOption) useDefaultKeyChain() bool {
	return o.Username == "" || o.Password == ""
}

type OCIImageFetcher struct {
	fetchOpt remote.Option
}

func NewOCIImageFetcher(opt OCIIMageFetcherOption) *OCIImageFetcher {
	var fetchOpt remote.Option
	// TODO(mathetake): have "Anonymous" option?
	if opt.useDefaultKeyChain() {
		// Note that defaukt key chain reads the docker config from DOCKER_CONFIG
		// so must set the envvar when reaching this branch is expected.
		fetchOpt = remote.WithAuthFromKeychain(authn.DefaultKeychain)
	} else {
		fetchOpt = remote.WithAuth(&authn.Basic{Username: opt.Username})
	}
	return &OCIImageFetcher{
		fetchOpt: fetchOpt,
	}
}

func (o *OCIImageFetcher) Fetch(url string) ([]byte, error) {
	ref, err := name.ParseReference(url)
	if err != nil {
		return nil, fmt.Errorf("could not parse url in image reference: %v", err)
	}

	img, err := remote.Image(ref, o.fetchOpt)
	if err != nil {
		return nil, fmt.Errorf("could not fetch image: %v", err)
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must be single-layered.
	if len(layers) != 1 {
		return nil, fmt.Errorf("number of layers must be one but got %d", len(layers))
	}

	layer := layers[0]
	mt, err := layer.MediaType()
	if err != nil {
		return nil, fmt.Errorf("could not get media type: %v", err)
	}

	if types.OCIUncompressedLayer != mt {
		return nil, fmt.Errorf("invalid media type %s", mt)
	}

	raw, err := layer.Uncompressed()
	if err != nil {
		return nil, fmt.Errorf("could not get layer content: %v", err)
	}

	defer raw.Close()

	ret, err := extractWasmExtensionFile(tar.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("could not extract wasm extension: %v", err)
	}

	// TODO(mathetake): Should we have a basic binary check? (e.g. Wasm header in binary.)
	// TODO(mathetake): Impl signature verification.

	return ret, nil
}

// Extracts the Wasm extension file in the archive named "extension.wasm".
func extractWasmExtensionFile(r *tar.Reader) ([]byte, error) {
	const wasmExtensionFileName = "extension.wasm"
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if h.Name == wasmExtensionFileName {
			return io.ReadAll(r)
		}
	}
	return nil, fmt.Errorf("%s not found in the archive", wasmExtensionFileName)
}
