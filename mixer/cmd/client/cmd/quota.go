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

package cmd

import (
	"context"
	"io"
	"strconv"

	"github.com/opentracing/opentracing-go/ext"
	"github.com/spf13/cobra"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/cmd/shared"
)

func quotaCmd(rootArgs *rootArgs, printf, fatalf shared.FormatFn) *cobra.Command {
	name := ""
	amount := int64(1)
	bestEffort := false

	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Invokes the mixer's Quota API.",
		Run: func(cmd *cobra.Command, args []string) {
			quota(rootArgs, printf, fatalf, name, amount, bestEffort)
		},
	}

	cmd.PersistentFlags().StringVarP(&name, "name", "", "", "The name of the quota to allocate")
	cmd.PersistentFlags().Int64VarP(&amount, "amount", "", 1, "The amount of quota to request")
	cmd.PersistentFlags().BoolVarP(&bestEffort, "bestEffort", "", false, "Whether to use all-or-nothing or best effort semantics")

	return cmd
}

func quota(rootArgs *rootArgs, printf, fatalf shared.FormatFn, name string, amount int64, bestEffort bool) {
	var attrs *mixerpb.Attributes
	var err error

	if attrs, err = parseAttributes(rootArgs); err != nil {
		fatalf("%v", err)
	}

	var cs *clientState
	if cs, err = createAPIClient(rootArgs.mixerAddress, rootArgs.enableTracing); err != nil {
		fatalf("Unable to establish connection to %s", rootArgs.mixerAddress)
	}
	defer deleteAPIClient(cs)

	span, ctx := cs.tracer.StartRootSpan(context.Background(), "mixc Quota", ext.SpanKindRPCClient)
	_, ctx = cs.tracer.PropagateSpan(ctx, span)

	var stream mixerpb.Mixer_QuotaClient
	if stream, err = cs.client.Quota(ctx); err != nil {
		fatalf("Quota RPC failed: %v", err)
	}

	for i := 0; i < rootArgs.repeat; i++ {
		dedup := strconv.Itoa(i)

		// send the request
		request := mixerpb.QuotaRequest{
			RequestIndex:    int64(i),
			AttributeUpdate: *attrs,
			Quota:           name,
			Amount:          amount,
			DeduplicationId: dedup,
			BestEffort:      bestEffort,
		}

		if err = stream.Send(&request); err != nil {
			fatalf("Failed to send Quota RPC: %v", err)
		}

		var response *mixerpb.QuotaResponse
		response, err = stream.Recv()
		if err == io.EOF {
			fatalf("Got no response from Quota RPC")
		} else if err != nil {
			fatalf("Failed to receive a response from Quota RPC: %v", err)
		}

		printf("Quota RPC returned %s, amount %v, expiration %v",
			decodeStatus(response.Result),
			response.Amount,
			response.Expiration)
		dumpAttributes(printf, fatalf, response.AttributeUpdate)
	}

	if err = stream.CloseSend(); err != nil {
		fatalf("Failed to close gRPC stream: %v", err)
	}

	span.Finish()
}
