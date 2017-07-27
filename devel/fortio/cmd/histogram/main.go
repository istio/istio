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

// histogram : reads values from stdin and outputs an histogram

package main

import (
	"bufio"
	"flag"
	"os"
	"strconv"

	"istio.io/istio/devel/fortio"
)

func main() {
	var (
		offsetFlag  = flag.Float64("offset", 0.0, "Offset for the data")
		dividerFlag = flag.Float64("divider", 1, "Divider/scaling for the data")
		pFlag       = flag.Float64("p", 90, "Percentile to calculate")
	)
	flag.Parse()
	h := fortio.NewHistogram(*offsetFlag, *dividerFlag)

	scanner := bufio.NewScanner(os.Stdin)
	linenum := 1
	for scanner.Scan() {
		line := scanner.Text()
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			fortio.Fatalf("Can't parse line %d: %v", linenum, err)
		}
		h.Record(v)
		linenum++
	}
	if err := scanner.Err(); err != nil {
		fortio.Fatalf("Err reading standard input %v", err)
	}
	// TODO use ParsePercentiles
	h.Print(os.Stdout, "Histogram", *pFlag)
}
