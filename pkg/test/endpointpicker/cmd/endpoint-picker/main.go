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

package main

import (
	"fmt"
	"io"
	"log"
	"net"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

type server struct {
	extprocv3.UnimplementedExternalProcessorServer
}

func (s *server) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	log.Printf("EPP: New stream connection established")
	requestCount := 0
	for {
		requestCount++
		log.Printf("EPP: Waiting to receive request #%d...", requestCount)
		req, err := stream.Recv()
		if err == io.EOF {
			log.Printf("EPP: Stream closed by client (EOF)")
			return nil
		}
		if err != nil {
			log.Printf("EPP: Error receiving request: %v", err)
			return err
		}

		log.Printf("EPP: Received request #%d, type: %T", requestCount, req.Request)
		var resp *extprocv3.ProcessingResponse
		var dynamicMetadata *structpb.Struct
		log.Printf("EPP: Request: %s", req.String())
		switch r := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			var selectedEndpoint string

			headers := r.RequestHeaders.Headers
			if headers != nil {
				log.Printf("EPP: Received %d headers", len(headers.Headers))
				// Check for x-endpoint header (client request for specific endpoint)
				for _, h := range headers.Headers {
					if h.Key == "x-endpoint" {
						selectedEndpoint = string(h.RawValue)
						log.Printf("EPP: Received x-endpoint header: %s", selectedEndpoint)
					}
					// Log x-envoy headers which may contain endpoint info
					if len(h.Key) > 7 && h.Key[:7] == "x-envoy" {
						log.Printf("EPP: Header %s: %s", h.Key, string(h.RawValue))
					}
				}
			}

			if selectedEndpoint == "" {
				// Default to a placeholder - in production this would query available endpoints
				selectedEndpoint = "10.0.0.1:8000"
				log.Printf("Using default endpoint: %s", selectedEndpoint)
			}

			// Build response according to EPP specification:
			// Set x-gateway-destination-endpoint in dynamic metadata (namespace: envoy.lb)
			// to communicate selected endpoint to data plane
			lbMetadata, err := structpb.NewStruct(map[string]interface{}{
				"x-gateway-destination-endpoint": selectedEndpoint,
			})
			if err != nil {
				log.Printf("Failed to create metadata: %v", err)
				resp = &extprocv3.ProcessingResponse{}
			} else {
				// Dynamic metadata must be set with namespace as top-level key
				var err error
				dynamicMetadata, err = structpb.NewStruct(map[string]interface{}{
					"envoy.lb": lbMetadata.AsMap(),
				})
				if err != nil {
					log.Printf("Failed to create dynamic metadata: %v", err)
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
					log.Printf("EPP response: set dynamic_metadata[envoy.lb][x-gateway-destination-endpoint]=%s and removed x-endpoint header", selectedEndpoint)
				}
			}

		case *extprocv3.ProcessingRequest_RequestBody:
			log.Printf("EPP: Processing RequestBody (streaming mode - no mutation)")
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
			log.Printf("EPP: Processing RequestTrailers (no mutation)")
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_RequestTrailers{
					RequestTrailers: &extprocv3.TrailersResponse{
						HeaderMutation: &extprocv3.HeaderMutation{},
					},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseHeaders:
			log.Printf("EPP: Processing ResponseHeaders (no mutation)")
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &extprocv3.HeadersResponse{
						Response: &extprocv3.CommonResponse{},
					},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseBody:
			log.Printf("EPP: Processing ResponseBody (streaming mode - no mutation)")
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
			log.Printf("EPP: Processing ResponseTrailers (no mutation)")
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseTrailers{
					ResponseTrailers: &extprocv3.TrailersResponse{
						HeaderMutation: &extprocv3.HeaderMutation{},
					},
				},
			}

		default:
			// For other request types, skip processing
			log.Printf("EPP: Received unknown request type: %T, skipping", req.Request)
			resp = &extprocv3.ProcessingResponse{}
		}

		log.Printf("EPP: Sending response for request #%d", requestCount)
		if err := stream.Send(resp); err != nil {
			log.Printf("EPP: Error sending response: %v", err)
			return err
		}
		log.Printf("EPP: Successfully sent response #%d", requestCount)
	}
}

func main() {
	port := 9002
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(s, &server{})

	log.Printf("Endpoint Picker gRPC server listening on port %d", port)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
