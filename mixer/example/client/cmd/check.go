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

// checkCmd represents the check command
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Invokes the mixer's Check API.",
	Run: func(cmd *cobra.Command, args []string) {
		cs, err := createAPIClient(MixerAddress)
		if err != nil {
			errorf("Unable to establish connection to %s", MixerAddress)
			return
		}
		defer deleteAPIClient(cs)

		stream, err := cs.client.Check(context.Background())
		if err != nil {
			errorf("Check RPC failed: %v", err)
			return
		}

		var attrs map[string]string
		if attrs, err = parseAttributes(Attributes); err != nil {
			errorf(err.Error())
			return
		}

		// send the request
		request := mixerpb.CheckRequest{RequestIndex: 0}

		// TODO: fix
		_ = attrs

		if err := stream.Send(&request); err != nil {
			errorf("Failed to send Check RPC: %v", err)
			return
		}

		response, err := stream.Recv()
		if err == io.EOF {
			errorf("Got no response from Check RPC")
			return
		} else if err != nil {
			errorf("Failed to receive a response from Check RPC: %v", err)
			return
		}
		stream.CloseSend()

		fmt.Printf("Check RPC returned %v\n", response.Result)
	},
}

func init() {
	RootCmd.AddCommand(checkCmd)
}
