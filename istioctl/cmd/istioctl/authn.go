// Copyright 2018 Istio Authors.
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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type AuthenticationDebug struct {
	Host                     string
	Port                     int
	AuthenticationPolicyName string
	DestinationRuleName      string
	ServerProtocol           string
	ClientProtocol           string
	TLSConflictStatus        string
}

// can allows user to query Istio RBAC effect for a specific request.
func check() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tls_check",
		Short: "Check whether TLS setting are matching between authentication policy and destination rules",
		Long: `
`,
		Example: ``,
		Args:    cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			pilots, err := getPilotPods()
			if err != nil {
				return err
			}
			if len(pilots) == 0 {
				return errors.New("unable to find any Pilot instances")
			}
			if debug, pilotErr := callPilotDiscoveryDebug(pilots, "", "authn"); pilotErr == nil {
				var dat []AuthenticationDebug
				if err := json.Unmarshal([]byte(debug), &dat); err != nil {
					panic(err)
				}
				sort.Slice(dat, func(i, j int) bool {
					if dat[i].Host == dat[j].Host {
						return dat[i].Port < dat[j].Port
					}
					return dat[i].Host < dat[j].Host
				})
				w := new(tabwriter.Writer)
				w.Init(os.Stdout, 0, 0, 4, ' ', 0)
				fmt.Fprintln(w, "Host:Port\tStatus\tServer\tClient\tAuthN Policy Name/Namespace\tDst Rule Name/Namespace")
				for _, entry := range dat {
					if entry.Host == "" {
						continue
					}
					host := fmt.Sprintf("%s:%5d", entry.Host, entry.Port)
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", host, entry.TLSConflictStatus,
						entry.ServerProtocol, entry.ClientProtocol,
						entry.AuthenticationPolicyName, entry.DestinationRuleName)
				}
				w.Flush()
			} else {
				fmt.Printf("%v\n", pilotErr)
			}

			return nil
		},
	}
	return cmd
}

// AuthN provides a command named authn that allows user to interact with Istio authentication policies.
func AuthN() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authn",
		Short: "Interact with Istio authentication policies",
		Long: `
`,
		Example: `# Check whether TLS setting are matching between authentication policy and destination rules:
istioctl authn tls_check`,
	}

	cmd.AddCommand(check())
	return cmd
}

func init() {
	rootCmd.AddCommand(AuthN())
}
