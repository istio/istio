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
	"os"

	"istio.io/pkg/log"
)

const (
	logFilePath = "./mesh-cli.log"
)

func getWriter(outFilename string) (*os.File, error) {
	writer := os.Stdout
	if outFilename != "" {
		file, err := os.Create(outFilename)
		if err != nil {
			return nil, err
		}

		writer = file
	}
	return writer, nil
}

func checkLogsOrExit(args *rootArgs) {
	if err := configLogs(args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}
}
func configLogs(args *rootArgs) error {
	opt := log.DefaultOptions()
	if !args.logToStdErr {
		opt.ErrorOutputPaths = []string{logFilePath}
		opt.OutputPaths = []string{logFilePath}
	}
	return log.Configure(opt)
}

// TODO: this really doesn't belong here. Figure out if it's generally needed and possibly move to istio.io/pkg/log.
func logAndPrintf(args *rootArgs, v ...interface{}) {
	s := fmt.Sprintf(v[0].(string), v[1:]...)
	if !args.logToStdErr {
		fmt.Println(s)
	}
	log.Infof(s)
}

func logAndFatalf(args *rootArgs, v ...interface{}) {
	logAndPrintf(args, v...)
	os.Exit(-1)
}
