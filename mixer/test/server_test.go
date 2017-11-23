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

package test

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	rpc "github.com/googleapis/googleapis/google/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/status"
)

var (
	attrs = mixerpb.CompressedAttributes{
		Words:   []string{"test.attribute", "test.value"},
		Strings: map[int32]int32{-1: -2},
	}

	testQuotas = map[string]mixerpb.CheckRequest_QuotaParams{
		"foo": {Amount: 55, BestEffort: false},
		"bar": {Amount: 21, BestEffort: true},
	}
)

type testSetupFn func(server *AttributesServer, handler *ChannelsHandler)

func noop(s *AttributesServer, h *ChannelsHandler) {}

func setGRPCErr(s *AttributesServer, h *ChannelsHandler) {
	s.GenerateGRPCError = true
}

func clearGRPCErr(s *AttributesServer, h *ChannelsHandler) {
	s.GenerateGRPCError = false
}

func setInvalidStatus(s *AttributesServer, h *ChannelsHandler) {
	h.ReturnStatus = status.WithInvalidArgument("test failure")
}

func clearStatus(s *AttributesServer, h *ChannelsHandler) {
	h.ReturnStatus = status.OK
}

func setQuotaResponse(s *AttributesServer, h *ChannelsHandler) {
	h.QuotaResponse = QuotaResponse{55 * time.Second, int64(999), nil}
}

func clearQuotaResponse(s *AttributesServer, h *ChannelsHandler) {
	h.QuotaResponse = QuotaResponse{DefaultValidDuration, DefaultAmount, nil}
}

func TestCheck(t *testing.T) {
	handler := NewChannelsHandler()
	attrSrv := NewAttributesServer(handler)
	grpcSrv, addr, err := startGRPCService(attrSrv)
	if err != nil {
		t.Fatalf("Could not start local grpc server: %v", err)
	}
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		conn.Close()
		grpcSrv.GracefulStop()
	}()

	client := mixerpb.NewMixerClient(conn)

	srcBag := attribute.NewProtoBag(&attrs, attrSrv.GlobalDict, attribute.GlobalList())
	wantBag := attribute.CopyBag(srcBag)

	noQuotaReq := &mixerpb.CheckRequest{Attributes: attrs}
	quotaReq := &mixerpb.CheckRequest{Attributes: attrs, Quotas: testQuotas, DeduplicationId: "baz"}

	refAttrs := srcBag.GetReferencedAttributes(attrSrv.GlobalDict, len(attribute.GlobalList()))

	okCheckResp := &mixerpb.CheckResponse{Precondition: precondition(status.OK, mixerpb.CompressedAttributes{}, refAttrs)}
	quotaResp := &mixerpb.CheckResponse{
		Precondition: precondition(status.OK, mixerpb.CompressedAttributes{}, refAttrs),
		Quotas: map[string]mixerpb.CheckResponse_QuotaResult{
			"foo": {ValidDuration: 55 * time.Second, GrantedAmount: 999, ReferencedAttributes: refAttrs},
			"bar": {ValidDuration: 55 * time.Second, GrantedAmount: 999, ReferencedAttributes: refAttrs},
		},
	}
	quotaDispatches := []QuotaDispatchInfo{
		{Attributes: wantBag, MethodArgs: QuotaArgs{"bazfoo", "foo", 55, false}},
		{Attributes: wantBag, MethodArgs: QuotaArgs{"bazbar", "bar", 21, true}},
	}

	cases := []struct {
		name           string
		req            *mixerpb.CheckRequest
		setupFn        testSetupFn
		teardownFn     testSetupFn
		wantCallErr    bool
		wantResponse   *mixerpb.CheckResponse
		wantAttributes attribute.Bag
		wantDispatches []QuotaDispatchInfo
	}{
		{"basic", noQuotaReq, noop, noop, false, okCheckResp, wantBag, nil},
		{"grpc err", noQuotaReq, setGRPCErr, clearGRPCErr, true, okCheckResp, nil, nil},
		{"check response", quotaReq, setQuotaResponse, clearQuotaResponse, false, quotaResp, wantBag, quotaDispatches},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			v.setupFn(attrSrv, handler)

			var wg sync.WaitGroup
			wg.Add(1)
			go func(req *mixerpb.CheckRequest, wantErr bool, wantResp *mixerpb.CheckResponse) {
				defer wg.Done()
				resp, err := client.Check(context.Background(), req)
				if wantErr {
					if err == nil {
						t.Error("No error in Check() call")
					}
					return
				}
				if err != nil {
					t.Errorf("Unexpected error in Check() call: %v", err)
					return
				}
				if !proto.Equal(resp, wantResp) {
					t.Errorf("Check() => %#v, \n\n wanted: %#v", resp, wantResp)
				}
			}(v.req, v.wantCallErr, v.wantResponse)

			if v.wantAttributes != nil {
				select {
				case got := <-handler.CheckAttributes:
					if !reflect.DeepEqual(got, v.wantAttributes) {
						t.Errorf("Check() => %v; want %v", got, v.wantAttributes)
					}
				case <-time.After(500 * time.Millisecond):
					t.Error("Check() => timed out waiting for attributes")
				}
			}

			// we have no control over order of emitted dispatches
			holder := make(map[string]QuotaDispatchInfo, len(v.wantDispatches))
			for range v.wantDispatches {
				select {
				case got := <-handler.QuotaDispatches:
					holder[got.MethodArgs.DeduplicationID] = got
				case <-time.After(500 * time.Millisecond):
					t.Error("Check() => timed out waiting for quota dispatches")
				}
			}

			for _, dispatch := range v.wantDispatches {
				got, found := holder[dispatch.MethodArgs.DeduplicationID]
				if !found {
					t.Errorf("No matching Quota dispatch found for: %v", dispatch)
					continue
				}
				if !reflect.DeepEqual(got, dispatch) {
					t.Errorf("Check() => %v; want %v", got, dispatch)
				}
			}

			wg.Wait()

			v.teardownFn(attrSrv, handler)
		})
	}
}

func TestReport(t *testing.T) {
	handler := NewChannelsHandler()
	attrSrv := NewAttributesServer(handler)
	grpcSrv, addr, err := startGRPCService(attrSrv)
	if err != nil {
		t.Fatalf("Could not start local grpc server: %v", err)
	}
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		err = conn.Close()
		grpcSrv.GracefulStop()
	}()

	client := mixerpb.NewMixerClient(conn)

	attrs := []mixerpb.CompressedAttributes{
		{
			Words:   []string{"some_user"},
			Strings: map[int32]int32{6: -1}, // 6 is global index for "source_user"
		},
		{
			Words:   []string{"updated_user", "another.attribute", "another.value"},
			Strings: map[int32]int32{6: -1, -2: -3}, // 6 is global index for "source_user"
		},
		{
			Strings: map[int32]int32{6: -1, -2: -3}, // 6 is global index for "source_user"
		},
	}

	words := []string{"foo", "bar", "baz"}

	baseBag := attribute.CopyBag(attribute.NewProtoBag(&attrs[0], attrSrv.GlobalDict, attribute.GlobalList()))
	middleBag := attribute.CopyBag(baseBag)
	if err = middleBag.UpdateBagFromProto(&attrs[1], attribute.GlobalList()); err != nil {
		t.Fatalf("Could not set up attribute bags for testing: %v", err)
	}

	finalAttr := &mixerpb.CompressedAttributes{Words: words, Strings: attrs[2].Strings}
	finalBag := attribute.CopyBag(middleBag)
	if err = finalBag.UpdateBagFromProto(finalAttr, attribute.GlobalList()); err != nil {
		t.Fatalf("Could not set up attribute bags for testing: %v", err)
	}

	attrBags := []attribute.Bag{baseBag, middleBag, finalBag}

	cases := []struct {
		name           string
		req            *mixerpb.ReportRequest
		setupFn        testSetupFn
		teardownFn     testSetupFn
		wantCallErr    bool
		wantAttributes []attribute.Bag
	}{
		{"basic", &mixerpb.ReportRequest{Attributes: attrs, DefaultWords: words}, noop, noop, false, attrBags},
		{"grpc err", &mixerpb.ReportRequest{Attributes: attrs, DefaultWords: words}, setGRPCErr, clearGRPCErr, true, nil},
		{"invalid status returned", &mixerpb.ReportRequest{Attributes: attrs, DefaultWords: words}, setInvalidStatus, clearStatus, true, attrBags},
		{"timeout", &mixerpb.ReportRequest{Attributes: attrs, DefaultWords: words}, noop, noop, true, nil},
		{"no attributes", &mixerpb.ReportRequest{}, noop, noop, false, []attribute.Bag{}},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {

			v.setupFn(attrSrv, handler)

			var wg sync.WaitGroup
			wg.Add(1)
			go func(req *mixerpb.ReportRequest, wantErr bool) {
				defer wg.Done()
				_, err := client.Report(context.Background(), req)
				if err == nil && wantErr {
					t.Error("No error in Report() call")
					return
				}
				if err != nil && !wantErr {
					t.Errorf("Unexpected error in Report() call: %v", err)
				}
			}(v.req, v.wantCallErr)

			for _, want := range v.wantAttributes {
				select {
				case got := <-handler.ReportAttributes:
					if !reflect.DeepEqual(got, want) {
						t.Errorf("Report() => %#v; want %#v", got, want)
					}
				case <-time.After(500 * time.Millisecond):
					t.Error("Report() => timed out waiting for report attributes")
				}
			}

			wg.Wait()

			v.teardownFn(attrSrv, handler)
		})
	}
}

func startGRPCService(attrSrv *AttributesServer) (*grpc.Server, string, error) {
	lis, port, err := ListenerAndPort()
	if err != nil {
		return nil, "", fmt.Errorf("could not find suitable listener: %v", err)
	}

	grpcSrv := NewMixerServer(attrSrv)

	go func() {
		_ = grpcSrv.Serve(lis)
	}()

	return grpcSrv, fmt.Sprintf("localhost:%d", port), nil
}

func precondition(status rpc.Status, attrs mixerpb.CompressedAttributes, refAttrs mixerpb.ReferencedAttributes) mixerpb.CheckResponse_PreconditionResult {
	return mixerpb.CheckResponse_PreconditionResult{
		Status:               status,
		ValidUseCount:        DefaultValidUseCount,
		ValidDuration:        DefaultValidDuration,
		Attributes:           attrs,
		ReferencedAttributes: refAttrs,
	}
}
