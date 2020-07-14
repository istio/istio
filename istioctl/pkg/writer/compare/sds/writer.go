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

package sdscompare

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// SDSWriter takes lists of SecretItem or SecretItemDiff and prints them through supplied output writer
type SDSWriter interface {
	PrintSecretItems([]SecretItem) error
	PrintDiffs([]SecretItemDiff) error
}

type Format int

const (
	JSON Format = iota
	TABULAR
)

// NewSDSWriter generates a new instance which conforms to SDSWriter interface
func NewSDSWriter(w io.Writer, format Format) SDSWriter {
	return &sdsWriter{
		w:      w,
		output: format,
	}
}

// sdsWriter is provided concrete implementation of SDSWriter
type sdsWriter struct {
	w      io.Writer
	output Format
}

// PrintSecretItems uses the user supplied output format to determine how to display the diffed secrets
func (w *sdsWriter) PrintSecretItems(secrets []SecretItem) error {
	var err error
	switch w.output {
	case JSON:
		err = w.printSecretItemsJSON(secrets)
	case TABULAR:
		err = w.printSecretItemsTabular(secrets)
	}
	return err
}

var (
	secretItemColumns = []string{"RESOURCE NAME", "TYPE", "STATUS", "VALID CERT", "SERIAL NUMBER", "NOT AFTER", "NOT BEFORE"}
	secretDiffColumns = []string{"RESOURCE NAME", "TYPE", "VALID CERT", "NODE AGENT", "PROXY", "SERIAL NUMBER", "NOT AFTER", "NOT BEFORE"}
)

// printSecretItemsTabular prints the secret in table format
func (w *sdsWriter) printSecretItemsTabular(secrets []SecretItem) error {
	if len(secrets) == 0 {
		fmt.Fprintln(w.w, "No secret items to show.")
		return nil
	}
	tw := new(tabwriter.Writer).Init(w.w, 0, 5, 5, ' ', 0)
	fmt.Fprintln(tw, strings.Join(secretItemColumns, "\t"))
	for _, s := range secrets {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%s\t%s\t%s\n",
			s.Name, s.Type, s.State, s.Valid, s.SerialNumber, s.NotAfter, s.NotBefore)
	}
	return tw.Flush()
}

// printSecretItemsJSON prints secret in JSON format, and dumps the raw certificate data with the output
func (w *sdsWriter) printSecretItemsJSON(secrets []SecretItem) error {
	out, err := json.MarshalIndent(secrets, "", " ")
	if err != nil {
		return err
	}

	_, err = w.w.Write(out)
	if err != nil {
		return err
	}

	return nil
}

// PrintDiffs uses the user supplied output format to determine how to display the diffed secrets
func (w *sdsWriter) PrintDiffs(statuses []SecretItemDiff) error {
	var err error
	switch w.output {
	case JSON:
		err = w.printDiffsJSON(statuses)
	case TABULAR:
		err = w.printDiffsTabular(statuses)
	}
	return err
}

// printsDiffsTabular prints the secret in table format
func (w *sdsWriter) printDiffsTabular(statuses []SecretItemDiff) error {
	if len(statuses) == 0 {
		fmt.Fprintln(w.w, "No secrets found to diff.")
		return nil
	}
	tw := new(tabwriter.Writer).Init(w.w, 0, 5, 5, ' ', 0)
	fmt.Fprintln(tw, strings.Join(secretDiffColumns, "\t"))
	for _, status := range statuses {
		fmt.Fprintf(tw, "%s\t%s\t%t\t%s\t%s\t%s\t%s\t%s\n",
			status.Name, status.Type, status.Valid, status.Source, status.Proxy, status.SerialNumber, status.NotAfter, status.NotBefore)
	}
	return tw.Flush()
}

// printDiffsJSON prints secret in JSON format, and dumps the raw certificate data with the output
func (w *sdsWriter) printDiffsJSON(statuses []SecretItemDiff) error {
	out, err := json.MarshalIndent(statuses, "", " ")
	if err != nil {
		return err
	}

	_, err = w.w.Write(out)
	if err != nil {
		return err
	}

	return nil
}
