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
package test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pkg/mcp/status"

	mcp "istio.io/api/mcp/v1alpha1"
)

type FakeTypeBase struct{ Info string }

func (f *FakeTypeBase) Reset()                   {}
func (f *FakeTypeBase) String() string           { return f.Info }
func (f *FakeTypeBase) ProtoMessage()            {}
func (f *FakeTypeBase) Marshal() ([]byte, error) { return []byte(f.Info), nil }
func (f *FakeTypeBase) Unmarshal(in []byte) error {
	f.Info = string(in)
	return nil
}

type FakeType0 struct{ FakeTypeBase }
type FakeType1 struct{ FakeTypeBase }
type FakeType2 struct{ FakeTypeBase }

type UnmarshalErrorType struct{ FakeTypeBase }

func (f *UnmarshalErrorType) Unmarshal(in []byte) error { return errors.New("unmarshal_error") }

const (
	TypePrefix                = "type.googleapis.com/"
	FakeType0MessageName      = "istio.io.galley.pkg.mcp.server.FakeType0"
	FakeType1MessageName      = "istio.io.galley.pkg.mcp.server.FakeType1"
	FakeType2MessageName      = "istio.io.galley.pkg.mcp.server.FakeType2"
	UnmarshalErrorMessageName = "istio.io.galley.pkg.mcp.server.UnmarshalErrorType"

	FakeType0TypeURL      = TypePrefix + FakeType0MessageName
	FakeType1TypeURL      = TypePrefix + FakeType1MessageName
	FakeType2TypeURL      = TypePrefix + FakeType2MessageName
	UnmarshalErrorTypeURL = TypePrefix + UnmarshalErrorMessageName
)

var (
	FakeType0Collection      = strings.Replace(FakeType0MessageName, ".", "/", -1)
	FakeType1Collection      = strings.Replace(FakeType1MessageName, ".", "/", -1)
	FakeType2Collection      = strings.Replace(FakeType2MessageName, ".", "/", -1)
	UnmarshalErrorCollection = strings.Replace(UnmarshalErrorMessageName, ".", "/", -1)
)

type Fake struct {
	Resource   *mcp.Resource
	Metadata   *mcp.Metadata
	Proto      proto.Message
	Collection string
	TypeURL    string
}

func MakeRequest(incremental bool, collection, nonce string, errorCode codes.Code) *mcp.RequestResources {
	req := &mcp.RequestResources{
		SinkNode:      Node,
		Collection:    collection,
		ResponseNonce: nonce,
		Incremental:   incremental,
	}
	if errorCode != codes.OK {
		req.ErrorDetail = status.New(errorCode, "").Proto()
	}
	return req
}

func MakeResources(incremental bool, collection, version, nonce string, removed []string, fakes ...*Fake) *mcp.Resources {
	r := &mcp.Resources{
		Collection:        collection,
		Nonce:             nonce,
		RemovedResources:  removed,
		SystemVersionInfo: version,
		Incremental:       incremental,
	}
	for _, fake := range fakes {
		r.Resources = append(r.Resources, *fake.Resource)
	}
	return r
}

func MakeFakeResource(collection, typeURL, version, name, data string) *Fake {
	var pb proto.Message
	switch typeURL {
	case FakeType0TypeURL:
		pb = &FakeType0{FakeTypeBase{data}}
	case FakeType1TypeURL:
		pb = &FakeType1{FakeTypeBase{data}}
	case FakeType2TypeURL:
		pb = &FakeType2{FakeTypeBase{data}}
	case UnmarshalErrorTypeURL:
		pb = &UnmarshalErrorType{FakeTypeBase{data}}
	default:
		panic(fmt.Sprintf("unknown typeURL: %v", typeURL))
	}

	body, err := types.MarshalAny(pb)
	if err != nil {
		panic(fmt.Sprintf("could not marshal fake body: %v", err))
	}

	metadata := &mcp.Metadata{Name: name, Version: version}

	envelope := &mcp.Resource{
		Metadata: metadata,
		Body:     body,
	}

	return &Fake{
		Resource:   envelope,
		Metadata:   metadata,
		Proto:      pb,
		TypeURL:    typeURL,
		Collection: collection,
	}
}

var (
	Type0A = []*Fake{}
	Type0B = []*Fake{}
	Type0C = []*Fake{}
	Type1A = []*Fake{}
	Type2A = []*Fake{}

	BadUnmarshal = MakeFakeResource(UnmarshalErrorCollection, UnmarshalErrorTypeURL, "v0", "bad", "data")

	SupportedCollections = []string{
		FakeType0Collection,
		FakeType1Collection,
		FakeType2Collection,
	}

	NodeID = "test-node"
	Node   = &mcp.SinkNode{
		Id: NodeID,
		Annotations: map[string]string{
			"foo": "bar",
		},
	}
	NodeMetadata = map[string]string{"foo": "bar"}
)

func init() {
	proto.RegisterType((*FakeType0)(nil), FakeType0MessageName)
	proto.RegisterType((*FakeType1)(nil), FakeType1MessageName)
	proto.RegisterType((*FakeType2)(nil), FakeType2MessageName)
	proto.RegisterType((*UnmarshalErrorType)(nil), UnmarshalErrorMessageName)

	Type0A = []*Fake{
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v0", "a", "data-a0"),
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v1", "a", "data-a1"),
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v2", "a", "data-a2"),
	}
	Type0B = []*Fake{
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v0", "b", "data-b0"),
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v1", "b", "data-b1"),
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v2", "b", "data-b2"),
	}
	Type0C = []*Fake{
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v0", "c", "data-c0"),
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v1", "c", "data-c1"),
		MakeFakeResource(FakeType0Collection, FakeType0TypeURL, "v2", "c", "data-c2"),
	}

	Type1A = []*Fake{
		MakeFakeResource(FakeType1Collection, FakeType1TypeURL, "v0", "a", "data-a0"),
		MakeFakeResource(FakeType1Collection, FakeType1TypeURL, "v1", "a", "data-a1"),
		MakeFakeResource(FakeType1Collection, FakeType1TypeURL, "v2", "a", "data-a2"),
	}

	Type2A = []*Fake{
		MakeFakeResource(FakeType2Collection, FakeType2TypeURL, "v0", "a", "data-a0"),
		MakeFakeResource(FakeType2Collection, FakeType2TypeURL, "v1", "a", "data-a1"),
		MakeFakeResource(FakeType2Collection, FakeType2TypeURL, "v2", "a", "data-a2"),
	}

	BadUnmarshal = MakeFakeResource(UnmarshalErrorCollection, UnmarshalErrorTypeURL, "v0", "bad", "data")
}
