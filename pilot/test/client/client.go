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

// An example implementation of a client.

package main

import (
	"context"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"crypto/tls"

	"github.com/golang/sync/errgroup"
)

var (
	count    int
	parallel bool
	insecure bool
	timeout  time.Duration
	client   *http.Client

	url       string
	headerKey string
	headerVal string
)

func init() {
	flag.IntVar(&count, "count", 1, "Number of times to make the request")
	flag.BoolVar(&parallel, "parallel", true, "Run requests in parallel")
	flag.BoolVar(&insecure, "insecure", false, "Insecure requests")
	flag.DurationVar(&timeout, "timeout", 60*time.Second, "Request timeout")
	flag.StringVar(&url, "url", "", "Specify URL")
	flag.StringVar(&headerKey, "key", "", "Header key")
	flag.StringVar(&headerVal, "val", "", "Header value")
}

func makeRequest(i int) func() error {
	return func() error {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}

		log.Printf("[%d] Url=%s\n", i, url)
		if headerKey != "" {
			req.Header.Add(headerKey, headerVal)
			log.Printf("[%d] Header=%s:%s\n", i, headerKey, headerVal)
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		log.Printf("[%d] StatusCode=%d\n", i, resp.StatusCode)

		data, err := ioutil.ReadAll(resp.Body)
		defer func() {
			if err = resp.Body.Close(); err != nil {
				log.Printf("[%d error] %s\n", i, err)
			}
		}()

		if err != nil {
			return err
		}

		for _, line := range strings.Split(string(data), "\n") {
			if line != "" {
				log.Printf("[%d body] %s\n", i, line)
			}
		}

		return nil
	}
}

func main() {
	flag.Parse()
	/* #nosec */
	client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecure,
			},
		},
		Timeout: timeout,
	}
	g, _ := errgroup.WithContext(context.Background())
	for i := 0; i < count; i++ {
		if parallel {
			g.Go(makeRequest(i))
		} else {
			err := makeRequest(i)()
			if err != nil {
				log.Printf("[%d error] %s\n", i, err)
			}
		}
	}
	if parallel {
		err := g.Wait()
		if err != nil {
			log.Printf("Error %s\n", err)
		} else {
			log.Println("All requests succeeded")
		}
	}
}
