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

func quotaCmd(rootArgs *rootArgs, errorf errorFn) *cobra.Command {
	name := ""
	dedup := ""
	amount := int64(1)
	bestEffort := false

	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Invokes the mixer's Quota API.",
		Run: func(cmd *cobra.Command, args []string) {
			quota(rootArgs, args, errorf, name, dedup, amount, bestEffort)
		},
	}

	cmd.PersistentFlags().StringVarP(&name, "name", "", "", "The name of the quota to allocate")
	cmd.PersistentFlags().Int64VarP(&amount, "amount", "", 1, "The amount of quota to request")
	cmd.PersistentFlags().StringVarP(&dedup, "dedup", "", "<generated>", "The deduplication id to use")
	cmd.PersistentFlags().BoolVarP(&bestEffort, "bestEffort", "", false, "Whether to use all-or-nothing or best effort semantics")

	return cmd
}

func quota(rootArgs *rootArgs, args []string, errorf errorFn, name string, dedup string, amount int64, bestEffort bool) {
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

	span, ctx := cs.tracer.StartRootSpan(context.Background(), "mixc Quota", ext.SpanKindRPCClient)
	_, ctx = cs.tracer.PropagateSpan(ctx, span)

	var stream mixerpb.Mixer_QuotaClient
	if stream, err = cs.client.Quota(ctx); err != nil {
		errorf("Quota RPC failed: %v", err)
		return
	}

	for i := 0; i < rootArgs.repeat; i++ {
		// send the request
		request := mixerpb.QuotaRequest{
			RequestIndex:    int64(i),
			AttributeUpdate: attrs,
			Quota:           name,
			Amount:          amount,
			DeduplicationId: dedup,
			BestEffort:      bestEffort,
		}

		if err = stream.Send(&request); err != nil {
			errorf("Failed to send Quota RPC: %v", err)
			break
		}

		var response *mixerpb.QuotaResponse
		response, err = stream.Recv()
		if err == io.EOF {
			errorf("Got no response from Quota RPC")
			break
		} else if err != nil {
			errorf("Failed to receive a response from Quota RPC: %v", err)
			break
		}

		fmt.Printf("Quota RPC returned %s, amount %v, expiration %v\n",
			decodeStatus(response.Result),
			response.Amount,
			response.Expiration)
	}

	if err = stream.CloseSend(); err != nil {
		errorf("Failed to close gRPC stream: %v", err)
	}

	span.Finish()
}
