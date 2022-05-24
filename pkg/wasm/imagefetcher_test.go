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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestImageFetcherOption_useDefaultKeyChain(t *testing.T) {
	cases := []struct {
		name string
		opt  ImageFetcherOption
		exp  bool
	}{
		{name: "default key chain", exp: true},
		{name: "use secret config", opt: ImageFetcherOption{PullSecret: []byte("secret")}},
		{name: "missing secret", opt: ImageFetcherOption{}, exp: true},
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

func TestImageFetcher_Fetch(t *testing.T) {
	// Fetcher with anonymous auth.
	fetcher := ImageFetcher{fetchOpts: []remote.Option{remote.WithAuth(authn.Anonymous)}}

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("docker image", func(t *testing.T) {
		ref := fmt.Sprintf("%s/test/valid/docker", u.Host)
		exp := "this is wasm plugin"

		// Create docker layer.
		l, err := newMockLayer(types.DockerLayer,
			map[string][]byte{"plugin.wasm": []byte(exp)})
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}

		// Set manifest type.
		manifest, err := img.Manifest()
		if err != nil {
			t.Fatal(err)
		}
		manifest.MediaType = types.DockerManifestSchema2

		// Push image to the registry.
		err = crane.Push(img, ref)
		if err != nil {
			t.Fatal(err)
		}

		// Fetch docker image with digest
		d, err := img.Digest()
		if err != nil {
			t.Fatal(err)
		}

		// Fetch OCI image.
		binaryFetcher, actualDiget, err := fetcher.PrepareFetch(ref)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := binaryFetcher()
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != exp {
			t.Errorf("ImageFetcher.binaryFetcher got %s, but want '%s'", string(actual), exp)
		}
		if actualDiget != d.Hex {
			t.Errorf("ImageFetcher.binaryFetcher got digest %s, but want '%s'", actualDiget, d.Hex)
		}
	})

	t.Run("OCI standard", func(t *testing.T) {
		ref := fmt.Sprintf("%s/test/valid/oci_standard", u.Host)
		exp := "this is wasm plugin"

		// Create OCI compressed layer.
		l, err := newMockLayer(types.OCILayer,
			map[string][]byte{"plugin.wasm": []byte(exp)})
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}
		img = mutate.MediaType(img, types.OCIManifestSchema1)

		// Push image to the registry.
		err = crane.Push(img, ref)
		if err != nil {
			t.Fatal(err)
		}

		// Fetch OCI image with digest
		d, err := img.Digest()
		if err != nil {
			t.Fatal(err)
		}

		// Fetch OCI image.
		binaryFetcher, actualDiget, err := fetcher.PrepareFetch(ref)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := binaryFetcher()
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != exp {
			t.Errorf("ImageFetcher.binaryFetcher got %s, but want '%s'", string(actual), exp)
		}
		if actualDiget != d.Hex {
			t.Errorf("ImageFetcher.binaryFetcher got digest %s, but want '%s'", actualDiget, d.Hex)
		}
	})

	t.Run("OCI artifact", func(t *testing.T) {
		ref := fmt.Sprintf("%s/test/valid/oci_artifact", u.Host)

		// Create the image with custom media types.
		wasmLayer, err := random.Layer(1000, "application/vnd.module.wasm.content.layer.v1+wasm")
		if err != nil {
			t.Fatal(err)
		}
		configLayer, err := random.Layer(1000, "application/vnd.module.wasm.config.v1+json")
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: wasmLayer}, mutate.Addendum{Layer: configLayer})
		if err != nil {
			t.Fatal(err)
		}
		img = mutate.MediaType(img, types.OCIManifestSchema1)

		// Push image to the registry.
		err = crane.Push(img, ref)
		if err != nil {
			t.Fatal(err)
		}

		// Retrieve the wanted image content.
		wantReader, err := wasmLayer.Compressed()
		if err != nil {
			t.Fatal(err)
		}
		defer wantReader.Close()

		want, err := io.ReadAll(wantReader)
		if err != nil {
			t.Fatal(err)
		}

		// Fetch OCI image with digest
		d, err := img.Digest()
		if err != nil {
			t.Fatal(err)
		}

		// Fetch OCI image.
		binaryFetcher, actualDiget, err := fetcher.PrepareFetch(ref)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := binaryFetcher()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, want) {
			t.Errorf("ImageFetcher.binaryFetcher got %s, but want '%s'", string(actual), string(want))
		}
		if actualDiget != d.Hex {
			t.Errorf("ImageFetcher.binaryFetcher got digest %s, but want '%s'", actualDiget, d.Hex)
		}
	})

	t.Run("invalid image", func(t *testing.T) {
		ref := fmt.Sprintf("%s/test/invalid", u.Host)

		l, err := newMockLayer(types.OCIUncompressedLayer, map[string][]byte{"not-wasm.txt": []byte("a")})
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}
		img = mutate.MediaType(img, types.OCIManifestSchema1)

		// Push image to the registry.
		err = crane.Push(img, ref)
		if err != nil {
			t.Fatal(err)
		}

		// Try to fetch.
		binaryFetcher, _, err := fetcher.PrepareFetch(ref)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := binaryFetcher()
		if actual != nil {
			t.Errorf("ImageFetcher.binaryFetcher got %s, but want nil", string(actual))
		}

		expErr := `the given image is in invalid format as an OCI image: 2 errors occurred:
	* could not parse as compat variant: invalid media type application/vnd.oci.image.layer.v1.tar (expect application/vnd.oci.image.layer.v1.tar+gzip)
	* could not parse as oci variant: number of layers must be 2 but got 1`
		if actual := strings.TrimSpace(err.Error()); actual != expErr {
			t.Errorf("ImageFetcher.binaryFetcher get unexpected error '%v', but want '%v'", actual, expErr)
		}
	})
}

func TestExtractDockerImage(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		exp := "this is wasm binary"
		l, err := newMockLayer(types.DockerLayer, map[string][]byte{
			"plugin.wasm": []byte(exp),
		})
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}
		actual, err := extractDockerImage(img)
		if err != nil {
			t.Fatalf("extractDockerImage failed: %v", err)
		}

		if string(actual) != exp {
			t.Fatalf("got %s, but want %s", string(actual), exp)
		}
	})

	t.Run("multiple layers", func(t *testing.T) {
		l, err := newMockLayer(types.DockerLayer, nil)
		if err != nil {
			t.Fatal(err)
		}
		img := empty.Image
		for i := 0; i < 2; i++ {
			img, err = mutate.Append(img, mutate.Addendum{Layer: l})
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err = extractDockerImage(img)
		if err == nil || !strings.Contains(err.Error(), "number of layers must be") {
			t.Fatal("extractDockerImage should fail due to invalid number of layers")
		}
	})

	t.Run("invalid media type", func(t *testing.T) {
		l, err := newMockLayer(types.DockerPluginConfig, nil)
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}
		_, err = extractDockerImage(img)
		if err == nil || !strings.Contains(err.Error(), "invalid media type") {
			t.Fatal("extractDockerImage should fail due to invalid media type")
		}
	})
}

func TestExtractOCIStandardImage(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		exp := "this is wasm binary"
		l, err := newMockLayer(types.OCILayer, map[string][]byte{
			"plugin.wasm": []byte(exp),
		})
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}
		actual, err := extractOCIStandardImage(img)
		if err != nil {
			t.Fatalf("extractOCIStandardImage failed: %v", err)
		}

		if string(actual) != exp {
			t.Fatalf("got %s, but want %s", string(actual), exp)
		}
	})

	t.Run("multiple layers", func(t *testing.T) {
		l, err := newMockLayer(types.OCILayer, nil)
		if err != nil {
			t.Fatal(err)
		}
		img := empty.Image
		for i := 0; i < 2; i++ {
			img, err = mutate.Append(img, mutate.Addendum{Layer: l})
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err = extractOCIStandardImage(img)
		if err == nil || !strings.Contains(err.Error(), "number of layers must be") {
			t.Fatal("extractOCIStandardImage should fail due to invalid number of layers")
		}
	})

	t.Run("invalid media type", func(t *testing.T) {
		l, err := newMockLayer(types.DockerLayer, nil)
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}
		_, err = extractOCIStandardImage(img)
		if err == nil || !strings.Contains(err.Error(), "invalid media type") {
			t.Fatal("extractOCIStandardImage should fail due to invalid media type")
		}
	})
}

func newMockLayer(mediaType types.MediaType, contents map[string][]byte) (v1.Layer, error) {
	var b bytes.Buffer
	hasher := sha256.New()
	mw := io.MultiWriter(&b, hasher)
	tw := tar.NewWriter(mw)
	defer tw.Close()

	for filename, content := range contents {
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
	return partial.UncompressedToLayer(
		&mockLayer{
			raw: b.Bytes(),
			diffID: v1.Hash{
				Algorithm: "sha256",
				Hex:       hex.EncodeToString(hasher.Sum(make([]byte, 0, hasher.Size()))),
			},
			mediaType: mediaType,
		},
	)
}

type mockLayer struct {
	raw       []byte
	diffID    v1.Hash
	mediaType types.MediaType
}

func (r *mockLayer) DiffID() (v1.Hash, error) { return v1.Hash{}, nil }
func (r *mockLayer) Uncompressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewBuffer(r.raw)), nil
}
func (r *mockLayer) MediaType() (types.MediaType, error) { return r.mediaType, nil }

func TestExtractOCIArtifactImage(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		// Create the image with custom media types.
		wasmLayer, err := random.Layer(1000, "application/vnd.module.wasm.content.layer.v1+wasm")
		if err != nil {
			t.Fatal(err)
		}
		configLayer, err := random.Layer(1000, "application/vnd.module.wasm.config.v1+json")
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: wasmLayer}, mutate.Addendum{Layer: configLayer})
		if err != nil {
			t.Fatal(err)
		}

		// Extract the binary.
		actual, err := extractOCIArtifactImage(img)
		if err != nil {
			t.Fatalf("extractOCIArtifactImage failed: %v", err)
		}

		// Retrieve the wanted image content.
		wantReader, err := wasmLayer.Compressed()
		if err != nil {
			t.Fatal(err)
		}
		defer wantReader.Close()
		want, err := io.ReadAll(wantReader)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(actual, want) {
			t.Errorf("extractOCIArtifactImage got %s, but want '%s'", string(actual), string(want))
		}
	})

	t.Run("invalid number of layers", func(t *testing.T) {
		l, err := random.Layer(1000, "application/vnd.module.wasm.content.layer.v1+wasm")
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
		if err != nil {
			t.Fatal(err)
		}
		_, err = extractOCIArtifactImage(img)
		if err == nil || !strings.Contains(err.Error(), "number of layers must be") {
			t.Fatal("extractOCIArtifactImage should fail due to invalid number of layers")
		}
	})

	t.Run("invalid media types", func(t *testing.T) {
		// Create the image with invalid media types.
		layer, err := random.Layer(1000, "aaa")
		if err != nil {
			t.Fatal(err)
		}
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: layer}, mutate.Addendum{Layer: layer})
		if err != nil {
			t.Fatal(err)
		}

		_, err = extractOCIArtifactImage(img)
		if err == nil || !strings.Contains(err.Error(),
			"could not find the layer of type application/vnd.module.wasm.content.layer.v1+wasm") {
			t.Fatal("extractOCIArtifactImage should fail due to invalid number of layers")
		}
	})
}

func TestExtractWasmPluginBinary(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		gz := gzip.NewWriter(buf)
		tw := tar.NewWriter(gz)

		exp := "hello"
		if err := tw.WriteHeader(&tar.Header{
			Name: "plugin.wasm",
			Size: int64(len(exp)),
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := io.WriteString(tw, exp); err != nil {
			t.Fatal(err)
		}

		tw.Close()
		gz.Close()

		actual, err := extractWasmPluginBinary(buf)
		if err != nil {
			t.Errorf("extractWasmPluginBinary failed: %v", err)
		}

		if string(actual) != exp {
			t.Errorf("extractWasmPluginBinary got %v, but want %v", string(actual), exp)
		}
	})

	t.Run("ok with relative path prefix", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		gz := gzip.NewWriter(buf)
		tw := tar.NewWriter(gz)

		exp := "hello"
		if err := tw.WriteHeader(&tar.Header{
			Name: "./plugin.wasm",
			Size: int64(len(exp)),
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := io.WriteString(tw, exp); err != nil {
			t.Fatal(err)
		}

		tw.Close()
		gz.Close()

		actual, err := extractWasmPluginBinary(buf)
		if err != nil {
			t.Errorf("extractWasmPluginBinary failed: %v", err)
		}

		if string(actual) != exp {
			t.Errorf("extractWasmPluginBinary got %v, but want %v", string(actual), exp)
		}
	})

	t.Run("not found", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		gz := gzip.NewWriter(buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{
			Name: "non-wasm.txt",
			Size: int64(1),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		tw.Close()
		gz.Close()
		_, err := extractWasmPluginBinary(buf)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("extractWasmPluginBinary must fail with not found")
		}
	})
}

func TestWasmKeyChain(t *testing.T) {
	dockerjson := fmt.Sprintf(`{"auths": {"test.io": {"auth": %q}}}`, encode("foo", "bar"))
	keyChain := wasmKeyChain{data: []byte(dockerjson)}
	testRegistry, _ := name.NewRegistry("test.io", name.WeakValidation)
	keyChain.Resolve(testRegistry)
	auth, err := keyChain.Resolve(testRegistry)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}
	got, err := auth.Authorization()
	if err != nil {
		t.Fatal(err)
	}
	want := &authn.AuthConfig{
		Username: "foo",
		Password: "bar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func encode(user, pass string) string {
	delimited := fmt.Sprintf("%s:%s", user, pass)
	return base64.StdEncoding.EncodeToString([]byte(delimited))
}
