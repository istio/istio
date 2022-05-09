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

package app

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pilot/pkg/request"
)

var (
	debugRequestPort int32 = 15000
	allowedPorts           = map[int32]struct{}{
		15000: {},
		15021: {},
		15020: {},
		15004: {},
	}
)

var allowedPortsString = func() string {
	s := []string{}
	for k := range allowedPorts {
		s = append(s, fmt.Sprint(k))
	}
	sort.Strings(s)
	return strings.Join(s, ", ")
}()

// NB: extra standard output in addition to what's returned from envoy
// must not be added in this command. Otherwise, it'd break istioctl proxy-config,
// which interprets the output literally as json document.
var (
	requestCmd = &cobra.Command{
		Use:   "request <method> <path> [<body>]",
		Short: "Makes an HTTP request to the Envoy admin API",
		Args:  cobra.MinimumNArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if _, f := allowedPorts[debugRequestPort]; !f {
				return fmt.Errorf("debug port %d is not in allowed list %v", debugRequestPort, allowedPortsString)
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			command := &request.Command{
				Address: fmt.Sprintf("localhost:%d", debugRequestPort),
				Client: &http.Client{
					Timeout: 60 * time.Second,
				},
			}
			body := ""
			if len(args) >= 3 {
				body = args[2]
			}
			return command.Do(args[0], args[1], body)
		},
	}
)

func init() {
	requestCmd.PersistentFlags().Int32Var(&debugRequestPort, "debug-port", debugRequestPort,
		"Set the port to make a local request to. The default points to the Envoy admin API.")
}
