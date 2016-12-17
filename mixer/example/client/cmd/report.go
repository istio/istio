// Copyright 2016 Google Inc.
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
	"fmt"
	"io"

	"github.com/spf13/cobra"

	mixerpb "istio.io/mixer/api/v1"
)

// reportCmd represents the report command
var reportCmd = &cobra.Command{
	Use:   "report <message>...",
	Short: "Invokes the mixer's Report API.",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			errorf("Message is missing.")
			return
		}

		cs, err := createAPIClient(MixerAddress)
		if err != nil {
			errorf("Unable to establish connection to %s: %v", MixerAddress, err)
			return
		}
		defer deleteAPIClient(cs)

		var attrs map[string]string
		if attrs, err = parseAttributes(Attributes); err != nil {
			errorf(err.Error())
			return
		}

		// TODO: fix
		_ = attrs

		stream, err := cs.client.Report(context.Background())
		if err != nil {
			errorf("Report RPC failed: %v", err)
			return
		}

		// send the request
		request := mixerpb.ReportRequest{RequestIndex: 0}
		/*
			request.Facts = attrs
			request.LogEntries = make([]*mixerpb.LogEntry, len(args))
			for i, arg := range args {
				now, _ := ptypes.TimestampProto(time.Now())
				request.LogEntries[i] = &mixerpb.LogEntry{
					Severity:  mixerpb.LogEntry_DEFAULT,
					Timestamp: now,
					Payload: &mixerpb.LogEntry_TextPayload{
						TextPayload: arg,
					},
				}
			}
		*/
		if err := stream.Send(&request); err != nil {
			errorf("Failed to send Report RPC: %v", err)
			return
		}

		response, err := stream.Recv()
		if err == io.EOF {
			errorf("Got no response from Report RPC")
			return
		} else if err != nil {
			errorf("Failed to receive a response from Report RPC: %v", err)
			return
		}
		stream.CloseSend()

		fmt.Printf("Report RPC returned %v\n", response.Result)
	}}

func init() {
	RootCmd.AddCommand(reportCmd)
}
