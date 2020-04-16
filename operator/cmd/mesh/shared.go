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

// Package mesh contains types and functions that are used across the full
// set of mixer commands.
package mesh

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

var (
	// Path to the operator install base dir in the snapshot. This symbol is required here because it's referenced
	// in "operator dump" e2e command tests and there's no other way to inject a path into the snapshot into the command.
	snapshotInstallPackageDir string
)

func initLogsOrExit(args *rootArgs) {
	if err := configLogs(args.logToStdErr, log.DefaultOptions()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}
}

func configLogs(logToStdErr bool, opt *log.Options) error {
	op := []string{"/dev/null"}
	if logToStdErr {
		op = []string{"stderr"}
	}
	opt2 := *opt
	opt2.OutputPaths = op
	opt2.ErrorOutputPaths = op

	return log.Configure(&opt2)
}

func refreshGoldenFiles() bool {
	return os.Getenv("REFRESH_GOLDEN") == "true"
}

func ReadLayeredYAMLs(filenames []string) (string, error) {
	return readLayeredYAMLs(filenames, os.Stdin)
}

func readLayeredYAMLs(filenames []string, stdinReader io.Reader) (string, error) {
	var ly string
	var stdin bool
	for _, fn := range filenames {
		var b []byte
		var err error
		if fn == "-" {
			if stdin {
				continue
			}
			stdin = true
			b, err = ioutil.ReadAll(stdinReader)
		} else {
			b, err = ioutil.ReadFile(strings.TrimSpace(fn))
		}
		if err != nil {
			return "", err
		}
		ly, err = util.OverlayYAML(ly, string(b))
		if err != nil {
			return "", err
		}
	}
	return ly, nil
}

// confirm waits for a user to confirm with the supplied message.
func confirm(msg string, writer io.Writer) bool {
	fmt.Fprintf(writer, "%s ", msg)

	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false
	}
	response = strings.ToUpper(response)
	if response == "Y" || response == "YES" {
		return true
	}

	return false
}
