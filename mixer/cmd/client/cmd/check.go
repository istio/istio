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

	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/spf13/cobra"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/cmd/shared"
)

func checkCmd(rootArgs *rootArgs, printf, fatalf shared.FormatFn) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Invokes Mixer's Check API to perform precondition checks.",
		Long: "The Check method is used to perform precondition checks. Mixer\n" +
			"expects a set of attributes as input, which it uses, along with\n" +
			"its configuration, to determine which adapters to invoke and with\n" +
			"which parameters in order to perform the precondition check.",

		Run: func(cmd *cobra.Command, args []string) {
			check(rootArgs, printf, fatalf)
		}}
}

func check(rootArgs *rootArgs, printf, fatalf shared.FormatFn) {
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

	span, ctx := ot.StartSpanFromContext(context.Background(), "mixc Check", ext.SpanKindRPCClient)
	for i := 0; i < rootArgs.repeat; i++ {
		request := mixerpb.CheckRequest{Attributes: *attrs}
		response, err := cs.client.Check(ctx, &request)

		printf("Check RPC returned %s, %s", decodeError(err), decodeStatus(response.Status))
		dumpAttributes(printf, fatalf, &response.Attributes)
	}

	span.Finish()
}
