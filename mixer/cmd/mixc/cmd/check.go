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
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/spf13/cobra"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/istio/pkg/log"
)

func checkCmd(rootArgs *rootArgs) *cobra.Command {
	quotas := ""

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Invokes Mixer's Check API to perform precondition checks and quota allocations.",
		Long: "The Check method is used to perform precondition checks and quota allocations. Mixer\n" +
			"expects a set of attributes as input, which it uses, along with\n" +
			"its configuration, to determine which adapters to invoke and with\n" +
			"which parameters in order to perform the checks and allocations.",
		Args: cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			q := make(map[string]int64)
			if len(quotas) > 0 {
				for _, seg := range strings.Split(quotas, ",") {
					eq := strings.Index(seg, "=")
					if eq < 0 {
						log.Fatalf("Quota definition %v does not include an = sign", seg)
					}
					if eq == 0 {
						log.Fatalf("Quota definition %v does not contain a valid name", seg)
					}
					name := seg[0:eq]
					value := seg[eq+1:]

					v, err := strconv.ParseInt(value, 10, 64)
					if err != nil {
						log.Fatalf("Unable to parse quota value %v: %v", value, err)
					}

					q[name] = v
				}
			}

			check(rootArgs, q)
		}}

	cmd.PersistentFlags().StringVarP(&quotas, "quotas", "q", "",
		"List of quotas to allocate specified as name1=amount1,name2=amount2,...")

	return cmd
}

func check(rootArgs *rootArgs, quotas map[string]int64) {
	var attrs *mixerpb.CompressedAttributes
	var err error

	if attrs, err = parseAttributes(rootArgs); err != nil {
		log.Fatalf("%v", err)
	}

	var cs *clientState
	if cs, err = createAPIClient(rootArgs.mixerAddress, rootArgs.tracingOptions); err != nil {
		log.Fatalf("Unable to establish connection to %s: %v", rootArgs.mixerAddress, err)
	}
	defer deleteAPIClient(cs)

	salt := time.Now().Nanosecond()
	span, ctx := ot.StartSpanFromContext(context.Background(), "mixc Check", ext.SpanKindRPCClient)
	for i := 0; i < rootArgs.repeat; i++ {
		dedup := strconv.Itoa(salt + i)

		request := mixerpb.CheckRequest{
			Attributes:      *attrs,
			DeduplicationId: dedup,
		}

		request.Quotas = make(map[string]mixerpb.CheckRequest_QuotaParams)
		for name, amount := range quotas {
			request.Quotas[name] = mixerpb.CheckRequest_QuotaParams{Amount: amount, BestEffort: true}
		}

		response, err := cs.client.Check(ctx, &request)

		if err == nil {
			log.Infof("Check RPC completed successfully. Check status was %s", decodeStatus(response.Precondition.Status))
			log.Infof("  Valid use count: %v, valid duration: %v", response.Precondition.ValidUseCount, response.Precondition.ValidDuration)
			dumpReferencedAttributes(response.Precondition.ReferencedAttributes)
			dumpQuotas(response.Quotas)
		} else {
			log.Errorf("Check RPC failed with: %s", decodeError(err))
		}
	}

	span.Finish()
}

func dumpQuotas(quotas map[string]mixerpb.CheckResponse_QuotaResult) {
	if quotas == nil {
		return
	}

	buf := bytes.Buffer{}
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprint(tw, "  Quota\tGranted Amount\tDuration\n")

	for name, qr := range quotas {
		fmt.Fprintf(tw, "  %s\t%v\t%v\n", name, qr.GrantedAmount, qr.ValidDuration)
	}

	_ = tw.Flush()
	log.Infof("%s", buf.String())
}
