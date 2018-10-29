// Copyright 2018 Istio Authors
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

// An example implementation of a client.

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"istio.io/istio/pkg/test/application/echo"
)

var (
	count     int
	timeout   time.Duration
	qps       int
	url       string
	headerKey string
	headerVal string
	headers   string
	msg       string

	caFile string
)

func init() {
	flag.IntVar(&count, "count", 1, "Number of times to make the request")
	flag.IntVar(&qps, "qps", 0, "Queries per second")
	flag.DurationVar(&timeout, "timeout", 15*time.Second, "Request timeout")
	flag.StringVar(&url, "url", "", "Specify URL")
	flag.StringVar(&headerKey, "key", "", "Header key (use Host for authority) - deprecated user headers instead")
	flag.StringVar(&headerVal, "val", "", "Header value - deprecated")
	flag.StringVar(&headers, "headers", "", "A list of http headers (use Host for authority) - name:value[,name:value]*")
	flag.StringVar(&caFile, "ca", "/cert.crt", "CA root cert file")
	flag.StringVar(&msg, "msg", "HelloWorld", "message to send (for websockets)")
}

func newBatchOptions() (echo.BatchOptions, error) {
	ops := echo.BatchOptions{
		URL:     url,
		Timeout: timeout,
		Count:   count,
		QPS:     qps,
		Message: msg,
		CAFile:  caFile,

		Header: make(http.Header),
	}

	// Old http add header - deprecated
	if headerKey != "" {
		ops.Header.Set(headerKey, headerVal)
	}

	if headers != "" {
		headersList := strings.Split(headers, ",")
		for _, header := range headersList {
			parts := strings.Split(header, ":")

			// require name:value format
			if len(parts) != 2 {
				return echo.BatchOptions{}, fmt.Errorf("invalid header format: %q (want name:value)", header)
			}
			ops.Header.Add(parts[0], parts[1])
		}
	}

	return ops, nil
}

func main() {
	flag.Parse()

	opts, err := newBatchOptions()
	if err != nil {
		log.Fatalf("Failed to setup client: %v", err)
		os.Exit(1)
	}

	b, err := echo.NewBatch(opts)
	if err != nil {
		log.Fatalf("Failed to setup client: %v", err)
		os.Exit(1)
	}

	// Run the batch.
	defer b.Close()
	output, err := b.Run()

	// Log the output
	for _, line := range output {
		// Lines are already terminated, no need for Println here.
		log.Print(line)
	}

	if err != nil {
		log.Printf("Error %s\n", err)
		os.Exit(1)
	}

	log.Println("All requests succeeded")
}
