// Copyright 2016 Istio Authors
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
	"context"
	"fmt"
	"io"

	"github.com/opentracing/opentracing-go/ext"
	"github.com/spf13/cobra"

	mixerpb "istio.io/api/mixer/v1"
)

func checkCmd(rootArgs *rootArgs, errorf errorFn) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Invokes the mixer's Check API.",
		Run: func(cmd *cobra.Command, args []string) {
			check(rootArgs, args, errorf)
		}}
}

func check(rootArgs *rootArgs, args []string, errorf errorFn) {
	var attrs *mixerpb.Attributes
	var err error

	if attrs, err = parseAttributes(rootArgs); err != nil {
		errorf(err.Error())
		return
	}

	var cs *clientState
	if cs, err = createAPIClient(rootArgs.mixerAddress, rootArgs.enableTracing); err != nil {
		errorf("Unable to establish connection to %s", rootArgs.mixerAddress)
		return
	}
	defer deleteAPIClient(cs)

	span, ctx := cs.tracer.StartRootSpan(context.Background(), "mixc Check", ext.SpanKindRPCClient)
	_, ctx = cs.tracer.PropagateSpan(ctx, span)

	var stream mixerpb.Mixer_CheckClient
	if stream, err = cs.client.Check(ctx); err != nil {
		errorf("Check RPC failed: %v", err)
		return
	}

	for i := 0; i < rootArgs.repeat; i++ {
		// send the request
		request := mixerpb.CheckRequest{RequestIndex: int64(i), AttributeUpdate: attrs}

		if err = stream.Send(&request); err != nil {
			errorf("Failed to send Check RPC: %v", err)
			break
		}

		var response *mixerpb.CheckResponse
		response, err = stream.Recv()
		if err == io.EOF {
			errorf("Got no response from Check RPC")
			break
		} else if err != nil {
			errorf("Failed to receive a response .from Check RPC: %v", err)
			break
		}

		fmt.Printf("Check RPC returned %s\n", decodeStatus(response.Result))
	}

	if err = stream.CloseSend(); err != nil {
		errorf("Failed to close gRPC stream: %v", err)
	}

	span.Finish()
}
