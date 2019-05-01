// Copyright 2019 Istio Authors. All Rights Reserved.
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
	"flag"
	"fmt"
	"os"

	"istio.io/istio/tools/checker"
)

func main() {
	flag.Parse()
	exitCode := 0

	items, err := getReport(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		exitCode = 2
	} else {
		for _, r := range items {
			fmt.Fprintln(os.Stderr, r)
			exitCode = 2
		}
	}

	os.Exit(exitCode)
}

func getReport(args []string) ([]string, error) {
	matcher := RulesMatcher{}
	whitelist := checker.NewWhitelist(Whitelist)
	report := checker.NewLintReport()

	err := checker.Check(args, &matcher, whitelist, report)
	if err != nil {
		return []string{}, err
	}
	return report.Items(), nil
}
