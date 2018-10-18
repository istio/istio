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

package main

// Will create the specified number of ADS connections to the pilot, and keep
// receiving notifications. This creates a load on pilot - without having to
// run a large number of pods in k8s.
// The clients will use 10.10.x.x addresses - it is possible to create ServiceEntry
// objects so clients get inbound listeners. Otherwise only outbound config will
// be pushed.

import (
	"flag"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"istio.io/istio/pkg/adsc"
)

var (
	// For local testing (single pilot) - better to keep it bellow 1000
	// As of Istio1.0.3, with a throttle of 250 qps push time for 1000 is ~15 seconds,
	// and with 500 qps push time is 28 seconds.
	//
	clients = flag.Int("clients",
		1000,
		"Number of ads clients.")

	pilotAddr = flag.String("pilot",
		"localhost:15010",
		"Pilot address. Can be a real pilot exposed for mesh expansion.")

	port = flag.String("port",
		":14000",
		"Http port for debug and control.")

	certDir = flag.String("certDir",
		"", // /etc/certs",
		"Certificate dir. Must be set according to mesh expansion docs for testing a meshex pilot.")
)

func main() {
	flag.Parse()

	for i := 0; i < *clients; i++ {
		n := i
		go runClient(n)
	}
	http.Handle("/metrics", promhttp.Handler())
	initMetrics()
	err := http.ListenAndServe(*port, nil)
	if err != nil {
		log.Fatal("failed to start monitoring port ", err)
	}
}

var (
	initialConnectz = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "connect",
		Help:    "Initial connection time, in ms",
		Buckets: []float64{100, 500, 1000, 2000, 5000, 10000, 20000, 40000},
	})

	connectedz = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "connected",
		Help: "Connected clients",
	})

	connectTimeoutz = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "connectTimeout",
		Help: "Connect timeouts",
	})

	cfgRecvz = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "configs",
		Help: "Received config types",
	}, []string{"type"})
)

func initMetrics() {
	prometheus.MustRegister(initialConnectz)
	prometheus.MustRegister(connectedz)
	prometheus.MustRegister(connectTimeoutz)
	prometheus.MustRegister(cfgRecvz)
}

// runClient creates a single long lived connection
func runClient(n int) {
	c, err := adsc.Dial(*pilotAddr, *certDir, &adsc.Config{
		IP: net.IPv4(10, 10, byte(n/256), byte(n%256)).String(),
	})
	if err != nil {
		log.Println("Error connecting ", err)
		return
	}

	t0 := time.Now()

	c.Watch()

	initialConnect := true
	_, err = c.Wait("rds", 30*time.Second)
	if err != nil {
		log.Println("Timeout receiving RDS")
		connectTimeoutz.Add(1)
		initialConnect = false
	} else {
		connectedz.Add(1)
	}

	ctime := time.Since(t0)
	initialConnectz.Observe(float64(ctime / 1000000)) // ms

	for {
		msg, err := c.Wait("", 15*time.Second)
		if err == adsc.ErrTimeout {
			continue
		}
		if msg == "close" {
			err = c.Reconnect()
			if err != nil {
				log.Println("Failed to reconnect")
				return
			}
		}
		if !initialConnect && msg == "rds" {
			// This is a delayed initial connect
			connectedz.Add(1)
			initialConnect = true
		}
		cfgRecvz.With(prometheus.Labels{"type": msg}).Add(1)
		log.Println("Received ", msg)
	}
}
