//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package logging

import (
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/impl/tmp"
)

var o = log.DefaultOptions()

// Attach log flags to the command-line.
func init() {
	c := cobra.Command{}
	o.AttachCobraFlags(&c)
	c.Flags().VisitAll(func(f *pflag.Flag) {
		// TODO: Transfer
		//fmt.Sprintf("%v\n", f)
	})
}

// Initialize sets the logging directory.
// Should be called right after flag.Parse().
func Initialize(runID string) error {
	// Create a temporary directory for any logging files.
	tmpDir, err := ioutil.TempDir(os.TempDir(), tmp.Prefix)
	if err != nil {
		return err
	}

	// Configure Istio logging to use a file under the temp dir.
	tmpLogFile, err := ioutil.TempFile(tmpDir, tmp.Prefix)
	if err != nil {
		return err
	}
	o.OutputPaths = []string{tmpLogFile.Name(), "stdout"}
	if err := log.Configure(o); err != nil {
		return err
	}

	log.Info("Logging initialized")
	return nil
}
