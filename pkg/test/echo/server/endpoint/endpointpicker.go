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

package endpoint

import (
	"fmt"
	"io"
	"net"
	"os"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
)

var eppLog = log.RegisterScope("epp", "endpoint picker")

// Neither header below is part of the endpoint picker protocol, which carries these values
// as envoy.lb dynamic metadata rather than as headers. They exist only so that a test client
// can read what this fake picker saw, and are named to make that obvious - nothing outside
// the test framework should depend on them.
const (
	// ServedEndpointHeader carries the endpoint Envoy reported as having served the request,
	// read from x-gateway-destination-endpoint-served. It distinguishes an endpoint that was
	// merely requested from the one that actually answered.
	ServedEndpointHeader = "x-test-served-endpoint"

	// PickerIDHeader names the pod running the picker that answered. A route rule may weight
	// traffic across several InferencePools and each pool's own picker has to score its own
	// share; the served endpoint alone cannot show which picker was consulted.
	PickerIDHeader = "x-test-picker-id"
)

type endpointPickerServer struct {
	extprocv3.UnimplementedExternalProcessorServer
	id string
}

func (s *endpointPickerServer) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	eppLog.Info("EPP: New stream connection established")
	requestCount := 0
	for {
		requestCount++
		eppLog.Debugf("EPP: Waiting to receive request #%d...", requestCount)
		req, err := stream.Recv()
		if err == io.EOF {
			eppLog.Debug("EPP: Stream closed by client (EOF)")
			return nil
		}
		if err != nil {
			eppLog.Errorf("EPP: Error receiving request: %v", err)
			return err
		}

		eppLog.Debugf("EPP: Received request #%d, type: %T", requestCount, req.Request)
		var resp *extprocv3.ProcessingResponse
		var dynamicMetadata *structpb.Struct
		eppLog.Debugf("EPP: Request: %s", req.String())
		switch r := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			var selectedEndpoint string

			headers := r.RequestHeaders.Headers
			if headers != nil {
				eppLog.Debugf("EPP: Received %d headers", len(headers.Headers))
				// Check for x-endpoint header (client request for specific endpoint)
				for _, h := range headers.Headers {
					if h.Key == "x-endpoint" {
						selectedEndpoint = string(h.RawValue)
						eppLog.Infof("EPP: Received x-endpoint header: %s", selectedEndpoint)
					}
					// Log x-envoy headers which may contain endpoint info
					if len(h.Key) > 7 && h.Key[:7] == "x-envoy" {
						eppLog.Debugf("EPP: Header %s: %s", h.Key, string(h.RawValue))
					}
				}
			}

			if selectedEndpoint == "" {
				// Default to a placeholder - in production this would query available endpoints
				selectedEndpoint = "10.0.0.1:8000"
				eppLog.Debugf("Using default endpoint: %s", selectedEndpoint)
			}

			// Build response according to EPP specification:
			// Set x-gateway-destination-endpoint in dynamic metadata (namespace: envoy.lb)
			// to communicate selected endpoint to data plane
			lbMetadata, err := structpb.NewStruct(map[string]interface{}{
				"x-gateway-destination-endpoint": selectedEndpoint,
			})
			if err != nil {
				eppLog.Errorf("Failed to create metadata: %v", err)
				resp = &extprocv3.ProcessingResponse{}
			} else {
				// Dynamic metadata must be set with namespace as top-level key
				var err error
				dynamicMetadata, err = structpb.NewStruct(map[string]interface{}{
					"envoy.lb": lbMetadata.AsMap(),
				})
				if err != nil {
					eppLog.Errorf("Failed to create dynamic metadata: %v", err)
					resp = &extprocv3.ProcessingResponse{}
				} else {
					resp = &extprocv3.ProcessingResponse{
						Response: &extprocv3.ProcessingResponse_RequestHeaders{
							RequestHeaders: &extprocv3.HeadersResponse{
								Response: &extprocv3.CommonResponse{
									HeaderMutation: &extprocv3.HeaderMutation{
										RemoveHeaders: []string{"x-endpoint"},
									},
								},
							},
						},
						DynamicMetadata: dynamicMetadata,
					}
					eppLog.Infof("EPP response: set dynamic_metadata[envoy.lb][x-gateway-destination-endpoint]=%s and removed x-endpoint header", selectedEndpoint)
				}
			}

		case *extprocv3.ProcessingRequest_RequestBody:
			eppLog.Debug("EPP: Processing RequestBody (streaming mode - no mutation)")
			// In FULL_DUPLEX_STREAMED mode, send BodyMutation with StreamedBodyResponse
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_RequestBody{
					RequestBody: &extprocv3.BodyResponse{
						Response: &extprocv3.CommonResponse{
							BodyMutation: &extprocv3.BodyMutation{
								Mutation: &extprocv3.BodyMutation_StreamedResponse{
									StreamedResponse: &extprocv3.StreamedBodyResponse{
										Body:        r.RequestBody.Body,
										EndOfStream: r.RequestBody.EndOfStream,
									},
								},
							},
						},
					},
				},
			}

		case *extprocv3.ProcessingRequest_RequestTrailers:
			eppLog.Debug("EPP: Processing RequestTrailers (no mutation)")
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_RequestTrailers{
					RequestTrailers: &extprocv3.TrailersResponse{
						HeaderMutation: &extprocv3.HeaderMutation{},
					},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseHeaders:
			// The override_host load balancer writes the host it dialled into envoy.lb - the
			// endpoint this picker asked for, or the fallback host when that endpoint was not
			// in the cluster's endpoint set. Surface it as a response header so a test can
			// tell those two apart without scraping proxy logs.
			served := servedEndpoint(req.GetMetadataContext())
			eppLog.Infof("EPP: ResponseHeaders, served endpoint: %q", served)
			headers := []*corev3.HeaderValueOption{{
				Header: &corev3.HeaderValue{Key: PickerIDHeader, RawValue: []byte(s.id)},
			}}
			if served != "" {
				headers = append(headers, &corev3.HeaderValueOption{
					Header: &corev3.HeaderValue{Key: ServedEndpointHeader, RawValue: []byte(served)},
				})
			}
			common := &extprocv3.CommonResponse{
				HeaderMutation: &extprocv3.HeaderMutation{SetHeaders: headers},
			}
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &extprocv3.HeadersResponse{
						Response: common,
					},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseBody:
			eppLog.Debug("EPP: Processing ResponseBody (streaming mode - no mutation)")
			// In FULL_DUPLEX_STREAMED mode, send BodyMutation with StreamedBodyResponse
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseBody{
					ResponseBody: &extprocv3.BodyResponse{
						Response: &extprocv3.CommonResponse{
							BodyMutation: &extprocv3.BodyMutation{
								Mutation: &extprocv3.BodyMutation_StreamedResponse{
									StreamedResponse: &extprocv3.StreamedBodyResponse{
										Body:        r.ResponseBody.Body,
										EndOfStream: r.ResponseBody.EndOfStream,
									},
								},
							},
						},
					},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseTrailers:
			eppLog.Debug("EPP: Processing ResponseTrailers (no mutation)")
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseTrailers{
					ResponseTrailers: &extprocv3.TrailersResponse{
						HeaderMutation: &extprocv3.HeaderMutation{},
					},
				},
			}

		default:
			// For other request types, skip processing
			eppLog.Warnf("EPP: Received unknown request type: %T, skipping", req.Request)
			resp = &extprocv3.ProcessingResponse{}
		}

		eppLog.Debugf("EPP: Sending response for request #%d", requestCount)
		if err := stream.Send(resp); err != nil {
			eppLog.Errorf("EPP: Error sending response: %v", err)
			return err
		}
		eppLog.Debugf("EPP: Successfully sent response #%d", requestCount)
	}
}

// endpointPickerInstance implements the Instance interface for endpoint picker
type endpointPickerInstance struct {
	Config
	server   *grpc.Server
	listener net.Listener
}

// newEndpointPicker creates a new endpoint picker endpoint instance.
func newEndpointPicker(config Config) *endpointPickerInstance {
	return &endpointPickerInstance{
		Config: config,
	}
}

func (e *endpointPickerInstance) Start(onReady OnReadyFunc) error {
	lis, port, err := listenOnAddress(e.ListenerIP, e.Port.Port)
	if err != nil {
		return fmt.Errorf("failed to listen for endpoint picker: %v", err)
	}
	e.listener = lis
	e.Port.Port = port

	e.server = grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(e.server, &endpointPickerServer{id: pickerID()})

	go func() {
		eppLog.Infof("Endpoint Picker gRPC server READY and listening on %s", lis.Addr().String())
		eppLog.Infof("Endpoint Picker is registered and waiting for ext_proc connections from Envoy")
		if err := e.server.Serve(lis); err != nil {
			eppLog.Errorf("Endpoint picker server failed: %v", err)
		}
		eppLog.Warnf("Endpoint Picker gRPC server stopped")
	}()

	onReady()
	return nil
}

func (e *endpointPickerInstance) Close() error {
	if e.server != nil {
		e.server.GracefulStop()
	}
	if e.listener != nil {
		return e.listener.Close()
	}
	return nil
}

func (e *endpointPickerInstance) GetConfig() Config {
	return e.Config
}

// servedEndpoint reads x-gateway-destination-endpoint-served out of the envoy.lb namespace
// of the metadata Envoy forwards with each processing request. An empty result means the
// key was absent, which is itself a signal: nothing reported the dialled host.
func servedEndpoint(md *corev3.Metadata) string {
	return md.GetFilterMetadata()[constants.EnvoySubsetNamespace].
		GetFields()[constants.GatewayInferenceExtensionEndpointServedKey].
		GetStringValue()
}

// pickerID identifies the pod this picker runs in. Tests map it back to the InferencePool
// the picker fronts.
func pickerID() string {
	host, err := os.Hostname()
	if err != nil {
		eppLog.Errorf("Failed to read hostname: %v", err)
		return "unknown"
	}
	return host
}
