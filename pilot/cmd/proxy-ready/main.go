// Copyright 2017 Aspen Mesh Authors
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
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var flagMin = flag.Int("n", 2, "minimum number of cds/lds/rds discovery successes required to be ready")
var flagPort = flag.Int("p", 15000, "proxy admin port")

func xdsCounts(in io.Reader) (cds, lds, rds int) {
	scanner := bufio.NewScanner(in)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ": ")
		if len(parts) == 2 {
			var err error
			switch parts[0] {
			case "cluster.rds.update_success":
				rds, err = strconv.Atoi(parts[1])
			case "cluster_manager.cds.update_success":
				cds, err = strconv.Atoi(parts[1])
			case "listener_manager.lds.update_success":
				lds, err = strconv.Atoi(parts[1])
			}
			if err != nil {
				log.Fatal(err)
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}
	return
}

func main() {

	flag.Parse()

	res, err := http.Get(fmt.Sprintf("http://localhost:%d/stats", *flagPort))
	if err != nil {
		log.Fatal(err)
	}

	cds, lds, rds := xdsCounts(res.Body)
	err = res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	if cds >= *flagMin && lds >= *flagMin && rds >= *flagMin {
		fmt.Printf("Initial discovery complete. cds: %d lds: %d rds: %d\n", cds, lds, rds)
		os.Exit(0)
	} else {
		fmt.Printf("Initial discovery pending. cds: %d lds: %d rds: %d\n", cds, lds, rds)
		os.Exit(1)
	}
}
