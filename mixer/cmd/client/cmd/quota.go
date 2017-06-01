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
	"strconv"
	"time"

	ot "github.com/opentracing/opentracing-go"
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
		Short: "Invokes Mixer's Quota API in order to perform quota management.",
		Long: "The Quota method is used to perform quota management. Mixer\n" +
			"expects a set of attributes as input, which it uses, along with\n" +
			"its configuration, to determine which adapters to invoke and with\n" +
			"which parameters in order to perform the quota operations.",

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

	span, ctx := ot.StartSpanFromContext(context.Background(), "mixc Quota", ext.SpanKindRPCClient)
	salt := time.Now().Nanosecond()

	for i := 0; i < rootArgs.repeat; i++ {
		dedup := strconv.Itoa(salt + i)

		// send the request
		request := mixerpb.QuotaRequest{
			Attributes:      *attrs,
			Quota:           name,
			Amount:          amount,
			DeduplicationId: dedup,
			BestEffort:      bestEffort,
		}

		response, err := cs.client.Quota(ctx, &request)

		printf("Quota RPC returned %s, amount %v, expiration %v",
			decodeError(err),
			response.Amount,
			response.Expiration)
	}

	span.Finish()
}
