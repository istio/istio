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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestOCIIMageFetcherOption_useDefaultKeyChain(t *testing.T) {
	cases := []struct {
		name string
		opt  OCIIMageFetcherOption
		exp  bool
	}{
		{name: "default key chain", exp: true},
		{name: "missing username", opt: OCIIMageFetcherOption{Password: "pass"}, exp: true},
		{name: "missing password", opt: OCIIMageFetcherOption{Username: "name"}, exp: true},
		{name: "use basic auth", opt: OCIIMageFetcherOption{Username: "name", Password: "pass"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := c.opt.useDefaultKeyChain()
			if actual != c.exp {
				t.Errorf("useDefaultKeyChain got %v want %v", actual, c.exp)
			}
		})
	}
}

func TestOCIImageFetcher_Fetch(t *testing.T) {
	// Fetcher anonymous auth.
	fetcher := OCIImageFetcher{fetchOpt: remote.WithAuth(authn.Anonymous)}

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Push an invalid (non Proxy-Wasm) image: non single layered.
	multiLayerImageRef := fmt.Sprintf("%s/test/multilayer", u.Host)
	img, err := random.Image(1024, 10)
	if err != nil {
		t.Fatal(err)
	}
	err = crane.Push(img, multiLayerImageRef)
	if err != nil {
		t.Fatal(err)
	}

	// Single Layer but the type is not "application/vnd.oci.image.layer.v1.tar"
	singleLayerInvalidTypeImageRef := fmt.Sprintf("%s/test/invalidtype", u.Host)
	layer, err := random.Layer(10, types.OCILayer)
	if err != nil {
		t.Fatal(err)
	}
	img, err = mutate.Append(empty.Image, mutate.Addendum{Layer: layer})
	if err != nil {
		t.Fatal(err)
	}
	err = crane.Push(img, singleLayerInvalidTypeImageRef)
	if err != nil {
		t.Fatal(err)
	}

	// Single Layer but does not contain "extension.wasm"
	invalidWasmPathImageRef := fmt.Sprintf("%s/test/valid", u.Host)
	tarLayer, err := newTarLayer(map[string][]byte{"invalid-path.wasm": []byte("a")})
	img, err = mutate.Append(empty.Image, mutate.Addendum{Layer: tarLayer})
	if err != nil {
		t.Fatal(err)
	}
	err = crane.Push(img, invalidWasmPathImageRef)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name, ref, exp string
	}{
		{name: "invalid multi layer", ref: multiLayerImageRef, exp: "number of layers must be one"},
		{name: "invalid media type", ref: singleLayerInvalidTypeImageRef, exp: "invalid media type"},
		{name: "invalid wasm path", ref: invalidWasmPathImageRef, exp: "extension.wasm not found in the archive"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err = fetcher.Fetch(c.ref)
			if err == nil || !strings.Contains(err.Error(), c.exp) {
				t.Errorf("Fetch must fail with erorr containing '%s'", c.exp)
			}
		})
	}

	// Push valid image.
	// TODO(mathetake): Use an actual Wasm binary?
	validImageRef := fmt.Sprintf("%s/test/valid", u.Host)
	exp := "this is wasm extension"
	tarLayer, err = newTarLayer(map[string][]byte{"extension.wasm": []byte(exp)})
	img, err = mutate.Append(empty.Image, mutate.Addendum{Layer: tarLayer})
	if err != nil {
		t.Fatal(err)
	}
	err = crane.Push(img, validImageRef)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid image", func(t *testing.T) {
		actual, err := fetcher.Fetch(validImageRef)
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != exp {
			t.Errorf("OCIImageFetcher.Fetch got %s, but want '%s'", string(actual), exp)
		}
	})
}

func newTarLayer(contents map[string][]byte) (v1.Layer, error) {
	var b bytes.Buffer
	hasher := sha256.New()
	mw := io.MultiWriter(&b, hasher)
	tw := tar.NewWriter(mw)
	defer tw.Close()

	for filename, content := range contents {
		// Write a single file with a random name and random contents.
		if err := tw.WriteHeader(&tar.Header{
			Name:     filename,
			Size:     int64(len(content)),
			Typeflag: tar.TypeRegA,
		}); err != nil {
			return nil, err
		}
		if _, err := io.CopyN(tw, bytes.NewReader(content), int64(len(content))); err != nil {
			return nil, err
		}
	}
	return partial.UncompressedToLayer(&tarLayer{raw: b.Bytes(), diffID: v1.Hash{
		Algorithm: "sha256",
		Hex:       hex.EncodeToString(hasher.Sum(make([]byte, 0, hasher.Size()))),
	}})
}

type tarLayer struct {
	raw    []byte
	diffID v1.Hash
}

// Implements partial.UncompressedLayer for tarLayer
func (r *tarLayer) DiffID() (v1.Hash, error) { return v1.Hash{}, nil }
func (r *tarLayer) Uncompressed() (io.ReadCloser, error) {
	return ioutil.NopCloser(bytes.NewBuffer(r.raw)), nil
}
func (r *tarLayer) MediaType() (types.MediaType, error) { return types.OCIUncompressedLayer, nil }

func TestExtractWasmExtensionFile(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		r := bytes.NewBuffer(nil)
		exp := "hello"
		w := tar.NewWriter(r)
		if err := w.WriteHeader(&tar.Header{
			Name: "extension.wasm",
			Size: int64(len(exp)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, exp); err != nil {
			t.Fatal(err)
		}

		actual, err := extractWasmExtensionFile(tar.NewReader(r))
		if err != nil {
			t.Errorf("extractWasmExtensionFile failed: %v", err)
		}

		if string(actual) != exp {
			t.Errorf("extractWasmExtensionFile got %v want %v", string(actual), exp)
		}
	})

	t.Run("not found", func(t *testing.T) {
		r := bytes.NewBuffer(nil)
		w := tar.NewWriter(r)
		if err := w.WriteHeader(&tar.Header{
			Name: "non-wasm.txt",
			Size: int64(1),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, "a"); err != nil {
			t.Fatal(err)
		}
		_, err := extractWasmExtensionFile(tar.NewReader(r))
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Error("extractWasmExtensionFile must fail with not found")
		}
	})
}
