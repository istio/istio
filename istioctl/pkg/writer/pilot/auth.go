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

package pilot

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

// TLSCheckWriter enables printing of tls-check using a single Pilot response
type TLSCheckWriter struct {
	Writer io.Writer
}

func (t *TLSCheckWriter) setupTLSCheckPrint(authDebug []byte) (*tabwriter.Writer, []v2.AuthenticationDebug, error) {
	var dat []v2.AuthenticationDebug
	if err := json.Unmarshal(authDebug, &dat); err != nil {
		return nil, nil, err
	}
	if len(dat) < 1 {
		return nil, nil, fmt.Errorf("nothing to output")
	}
	sort.Slice(dat, func(i, j int) bool {
		if dat[i].Host == dat[j].Host {
			return dat[i].Port < dat[j].Port
		}
		return dat[i].Host < dat[j].Host
	})
	w := new(tabwriter.Writer).Init(t.Writer, 0, 8, 5, ' ', 0)
	fmt.Fprintln(w, "HOST:PORT\tSTATUS\tSERVER\tCLIENT\tAUTHN POLICY\tDESTINATION RULE")
	return w, dat, nil
}

// PrintAll takes a Pilot authenticationz response and outputs them using a tabwriter
func (t *TLSCheckWriter) PrintAll(authDebug []byte) error {
	w, fullAuth, err := t.setupTLSCheckPrint(authDebug)
	if err != nil {
		return err
	}
	for _, entry := range fullAuth {
		tlsCheckPrintln(w, entry)
	}
	return w.Flush()
}

// PrintSingle takes a Pilot authenticationz response and outputs them using a tabwriter filtering for a specific service
func (t *TLSCheckWriter) PrintSingle(authDebug []byte, service string) error {
	w, fullAuth, err := t.setupTLSCheckPrint(authDebug)
	if err != nil {
		return err
	}
	for _, entry := range fullAuth {
		if entry.Host == service {
			tlsCheckPrintln(w, entry)
			break
		}
	}
	return w.Flush()
}

func tlsCheckPrintln(w io.Writer, entry v2.AuthenticationDebug) {
	if entry.Host == "" {
		return
	}
	host := fmt.Sprintf("%s:%5d", entry.Host, entry.Port)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", host, entry.TLSConflictStatus,
		entry.ServerProtocol, entry.ClientProtocol,
		entry.AuthenticationPolicyName, entry.DestinationRuleName)
}
