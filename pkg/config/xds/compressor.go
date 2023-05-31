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

package xds

import (
	"errors"
	"fmt"

	brotli_compressor "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/brotli/compressor/v3"
	gzip_compressor "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/gzip/compressor/v3"
	zstd_compressor "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/zstd/compressor/v3"
	compressor "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/compressor/v3"
	"google.golang.org/protobuf/proto"
)

// Compressor Envoy HTTP filter description.
type Compressor struct{}

func init() {
	initRegister(&Compressor{})
}

func (_ *Compressor) Name() string {
	return "compressor"
}

func (_ *Compressor) TypeURL() string {
	return "type.googleapis.com/envoy.extensions.filters.http.compressor.v3.Compressor"
}

func (_ *Compressor) New() proto.Message {
	return &compressor.Compressor{}
}

const (
	gzip   = "type.googleapis.com/envoy.extensions.compression.gzip.compressor.v3.Gzip"
	brotli = "type.googleapis.com/envoy.extensions.compression.brotli.compressor.v3.Brotli"
	zstd   = "type.googleapis.com/envoy.extensions.compression.zstd.compressor.v3.Zstd"
)

func (_ *Compressor) Validate(pb proto.Message) error {
	// Allow only stable compression algorithms.
	config := pb.(*compressor.Compressor)
	library := config.CompressorLibrary
	if library == nil {
		return errors.New("empty compressor library")
	}
	extension := library.TypedConfig
	if extension == nil {
		return errors.New("empty compressor extension")
	}
	switch alg := extension.TypeUrl; alg {
	case gzip:
		out := &gzip_compressor.Gzip{}
		return proto.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(extension.Value, out)
	case brotli:
		out := &brotli_compressor.Brotli{}
		return proto.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(extension.Value, out)
	case zstd:
		out := &zstd_compressor.Zstd{}
		return proto.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(extension.Value, out)
	default:
		return fmt.Errorf("unknown compression algorithm: %s", alg)
	}
}
