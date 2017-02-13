// Copyright 2017 Google Inc.
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
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var requests = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "request_count",
		Help: "requests",
	},
	[]string{"source", "target", "service"},
)

func init() {
	prometheus.MustRegister(requests)
}

func incrReq(source, target, service string) {
	requests.WithLabelValues(source, target, service).Inc()
}

func main() {
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < 10; i++ {
		go func() {
			c := time.Tick(50 * time.Millisecond)
			for range c {
				s := rand.Intn(3)
				t := rand.Intn(3)
				for s == t {
					t = rand.Intn(3)
				}
				source := fmt.Sprintf("App %d", s)
				target := fmt.Sprintf("App %d", t)
				service := fmt.Sprintf("Service %d", rand.Intn(5))
				incrReq(source, target, service)
			}
		}()
	}

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":8084", nil))
}
