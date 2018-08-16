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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"istio.io/fortio/log"
	"istio.io/fortio/stats"
)

func main() {
	var (
		offsetFlag      = flag.Float64("offset", 0.0, "Offset for the data")
		dividerFlag     = flag.Float64("divider", 1, "Divider/scaling for the data")
		percentilesFlag = flag.String("p", "50,75,99,99.9", "List of pXX to calculate")
		jsonFlag        = flag.Bool("json", false, "Json output")
	)
	flag.Parse()
	h := stats.NewHistogram(*offsetFlag, *dividerFlag)
	percList, err := stats.ParsePercentiles(*percentilesFlag)
	if err != nil {
		log.Fatalf("Unable to extract percentiles from -p: %v", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	linenum := 1
	for scanner.Scan() {
		line := scanner.Text()
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			log.Fatalf("Can't parse line %d: %v", linenum, err)
		}
		h.Record(v)
		linenum++
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Err reading standard input %v", err)
	}
	if *jsonFlag {
		b, err := json.MarshalIndent(h.Export().CalcPercentiles(percList), "", "  ")
		if err != nil {
			log.Fatalf("Unable to create Json: %v", err)
		}
		fmt.Print(string(b))
	} else {
		h.Print(os.Stdout, "Histogram", percList)
	}
}
