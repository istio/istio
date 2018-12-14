// Copyright 2017 Istio Authors
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

// Package mockapi supplies a fake Mixer server for use in testing. It should NOT
// be used outside of testing contexts.
package mockapi // import "istio.io/istio/mixer/pkg/mockapi"

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"time"

	rpc "github.com/gogo/googleapis/google/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/status"
)

// DefaultAmount is the default quota amount to use in testing (1).
var DefaultAmount = int64(1)

// DefaultValidUseCount is the default number of valid uses to return for
// quota allocs for testing (1).
var DefaultValidUseCount = int32(10000)

// DefaultValidDuration is the default duration to return for
// quota allocs in testing (1s).
var DefaultValidDuration = 5 * time.Second

// AttributesServer implements the Mixer API to send mutable attributes bags to
// a channel upon API requests. This can be used for tests that want to exercise
// the Mixer API and validate server handling of supplied attributes.
type AttributesServer struct {
	// GlobalDict controls the known global dictionary for attribute processing.
	GlobalDict map[string]int32

	// GenerateGRPCError instructs the server whether or not to fail-fast with
	// an error that will manifest as a GRPC error.
	GenerateGRPCError bool

	// Handler is what the server will call to simulate passing attribute bags
	// and method args within the Mixer server. It allows tests to gain access
	// to the attribute handling pipeline within Mixer and to set the response
	// details.
	Handler AttributesHandler

	// CheckGlobalDict indicates whether to check if proxy global dictionary
	// is ahead of the one in mixer.
	checkGlobalDict bool

	// CheckMetadata indicates whether to check for presence of gRPC metadata for
	// forwarded attributes.
	checkMetadata func(*mixerpb.Attributes) error
}

// NewAttributesServer creates an AttributesServer. All channels are set to
// default length.
func NewAttributesServer(handler AttributesHandler, checkDict bool) *AttributesServer {
	list := attribute.GlobalList()
	globalDict := make(map[string]int32, len(list))
	for i := 0; i < len(list); i++ {
		globalDict[list[i]] = int32(i)
	}

	return &AttributesServer{
		GlobalDict:        globalDict,
		GenerateGRPCError: false,
		Handler:           handler,
		checkGlobalDict:   checkDict,
	}
}

// SetCheckMetadata enables gRPC metadata checking.
func (a *AttributesServer) SetCheckMetadata(checkMetadata func(*mixerpb.Attributes) error) {
	a.checkMetadata = checkMetadata
}

func (a *AttributesServer) validateMetadata(ctx context.Context) error {
	headers, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return errors.New("no gRPC metadata in the incoming context")
	}
	header := headers.Get("x-istio-attributes")
	if len(header) != 1 {
		return fmt.Errorf("incorrect x-istio-attributes metadata in gRPC context: %v", header)
	}
	decoded, err := base64.StdEncoding.DecodeString(header[0])
	if err != nil {
		return err
	}
	var attrs mixerpb.Attributes
	if err = attrs.Unmarshal(decoded); err != nil {
		return err
	}
	if err = a.checkMetadata(&attrs); err != nil {
		return err
	}
	return nil
}

// Check sends a copy of the protocol buffers attributes wrapper for the preconditions
// check as well as for each quotas check to the CheckAttributes channel. It also
// builds a CheckResponse based on server fields. All channel sends timeout to
// prevent problematic tests from blocking indefinitely.
func (a *AttributesServer) Check(ctx context.Context, req *mixerpb.CheckRequest) (*mixerpb.CheckResponse, error) {
	if a.checkMetadata != nil {
		if err := a.validateMetadata(ctx); err != nil {
			return nil, err
		}
	}

	if a.GenerateGRPCError {
		return nil, errors.New("error handling check call")
	}
	if a.checkGlobalDict && req.GlobalWordCount > uint32(len(a.GlobalDict)) {
		return nil, fmt.Errorf("global dictionary mismatch: proxy %d and mixer %d", req.GlobalWordCount, len(a.GlobalDict))
	}

	requestBag := attribute.NewProtoBag(&req.Attributes, a.GlobalDict, attribute.GlobalList())
	defer requestBag.Done()

	result := a.Handler.Check(requestBag)
	if result.ReferencedAttributes == nil {
		result.ReferencedAttributes = requestBag.GetReferencedAttributes(a.GlobalDict, int(req.GlobalWordCount))
	}
	requestBag.ClearReferencedAttributes()

	resp := &mixerpb.CheckResponse{Precondition: result}

	if len(req.Quotas) > 0 {
		resp.Quotas = make(map[string]mixerpb.CheckResponse_QuotaResult, len(req.Quotas))
		for name, param := range req.Quotas {
			args := QuotaArgs{
				Quota:           name,
				Amount:          param.Amount,
				DeduplicationID: req.DeduplicationId + name,
				BestEffort:      param.BestEffort,
			}

			result, out := a.Handler.Quota(requestBag, args)
			if status.IsOK(resp.Precondition.Status) && !status.IsOK(out) {
				resp.Precondition.Status = out
			}

			qr := mixerpb.CheckResponse_QuotaResult{
				GrantedAmount:        result.Amount,
				ValidDuration:        result.Expiration,
				ReferencedAttributes: *requestBag.GetReferencedAttributes(a.GlobalDict, int(req.GlobalWordCount)),
			}
			if result.Referenced != nil {
				qr.ReferencedAttributes = *result.Referenced
			} else {
				qr.ReferencedAttributes = *requestBag.GetReferencedAttributes(a.GlobalDict, int(req.GlobalWordCount))
			}
			resp.Quotas[name] = qr
			requestBag.ClearReferencedAttributes()
		}
	}
	return resp, nil
}

// Report iterates through the supplied attributes sets, applying the deltas
// appropriately, and sending the generated bags to the channel.
func (a *AttributesServer) Report(ctx context.Context, req *mixerpb.ReportRequest) (*mixerpb.ReportResponse, error) {
	if a.checkMetadata != nil {
		if err := a.validateMetadata(ctx); err != nil {
			return nil, err
		}
	}

	if a.GenerateGRPCError {
		return nil, errors.New("error handling report call")
	}

	if len(req.Attributes) == 0 {
		// early out
		return &mixerpb.ReportResponse{}, nil
	}
	// apply the request-level word list to each attribute message if needed
	for i := 0; i < len(req.Attributes); i++ {
		if len(req.Attributes[i].Words) == 0 {
			req.Attributes[i].Words = req.DefaultWords
		}
	}

	protoBag := attribute.NewProtoBag(&req.Attributes[0], a.GlobalDict, attribute.GlobalList())
	requestBag := attribute.GetMutableBag(protoBag)
	defer requestBag.Done()
	defer protoBag.Done()

	out := a.Handler.Report(requestBag)

	for i := 1; i < len(req.Attributes); i++ {
		// the first attribute block is handled by the protoBag as a foundation,
		// deltas are applied to the child bag (i.e. requestBag)
		if err := requestBag.UpdateBagFromProto(&req.Attributes[i], attribute.GlobalList()); err != nil {
			return &mixerpb.ReportResponse{}, fmt.Errorf("could not apply attribute delta: %v", err)
		}

		out = a.Handler.Report(requestBag)
	}

	if !status.IsOK(out) {
		return nil, makeGRPCError(out)
	}

	return &mixerpb.ReportResponse{}, nil
}

// NewMixerServer creates a new grpc.Server with the supplied implementation
// of the Mixer API.
func NewMixerServer(impl mixerpb.MixerServer) *grpc.Server {
	gs := grpc.NewServer()
	mixerpb.RegisterMixerServer(gs, impl)
	return gs
}

// ListenerAndPort starts a listener on an available port and returns both the
// listener and the port on which it is listening.
func ListenerAndPort() (net.Listener, int, error) {
	lis, err := net.Listen("tcp", ":0") // nolint: gas
	if err != nil {
		return nil, 0, fmt.Errorf("could not find open port for server: %v", err)
	}
	return lis, lis.Addr().(*net.TCPAddr).Port, nil
}

func makeGRPCError(status rpc.Status) error {
	return grpcstatus.Errorf(codes.Code(status.Code), status.Message)
}
