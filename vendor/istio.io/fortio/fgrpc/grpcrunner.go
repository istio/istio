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

package fgrpc // import "istio.io/fortio/fgrpc"

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"

	"strings"

	"istio.io/fortio/fnet"
	"istio.io/fortio/log"
	"istio.io/fortio/periodic"
)

// Dial dials grpc using insecure or tls transport security when serverAddr
// has prefixHTTPS or cert is provided. If override is set to a non empty string,
// it will override the virtual host name of authority in requests.
func Dial(o *GRPCRunnerOptions) (conn *grpc.ClientConn, err error) {
	var opts []grpc.DialOption
	switch {
	case o.CACert != "":
		var creds credentials.TransportCredentials
		creds, err = credentials.NewClientTLSFromFile(o.CACert, o.CertOverride)
		if err != nil {
			log.Errf("Invalid TLS credentials: %v\n", err)
			return nil, err
		}
		log.Infof("Using CA certificate %v to construct TLS credentials", o.CACert)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	case strings.HasPrefix(o.Destination, fnet.PrefixHTTPS):
		creds := credentials.NewTLS(nil)
		opts = append(opts, grpc.WithTransportCredentials(creds))
	default:
		opts = append(opts, grpc.WithInsecure())
	}
	serverAddr := grpcDestination(o.Destination)
	if o.UnixDomainSocket != "" {
		log.Warnf("Using domain socket %v instead of %v for grpc connection", o.UnixDomainSocket, serverAddr)
		opts = append(opts, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout(fnet.UnixDomainSocket, o.UnixDomainSocket, timeout)
		}))
	}
	conn, err = grpc.Dial(serverAddr, opts...)
	if err != nil {
		log.Errf("failed to connect to %s with certificate %s and override %s: %v", serverAddr, o.CACert, o.CertOverride, err)
	}
	return conn, err
}

// TODO: refactor common parts between http and grpc runners

// GRPCRunnerResults is the aggregated result of an GRPCRunner.
// Also is the internal type used per thread/goroutine.
type GRPCRunnerResults struct {
	periodic.RunnerResults
	clientH     grpc_health_v1.HealthClient
	reqH        grpc_health_v1.HealthCheckRequest
	clientP     PingServerClient
	reqP        PingMessage
	RetCodes    HealthResultMap
	Destination string
	Streams     int
	Ping        bool
}

// Run exercises GRPC health check or ping at the target QPS.
// To be set as the Function in RunnerOptions.
func (grpcstate *GRPCRunnerResults) Run(t int) {
	log.Debugf("Calling in %d", t)
	var err error
	var res interface{}
	status := grpc_health_v1.HealthCheckResponse_SERVING
	if grpcstate.Ping {
		res, err = grpcstate.clientP.Ping(context.Background(), &grpcstate.reqP)
	} else {
		var r *grpc_health_v1.HealthCheckResponse
		r, err = grpcstate.clientH.Check(context.Background(), &grpcstate.reqH)
		if r != nil {
			status = r.Status
			res = r
		}
	}
	log.Debugf("For %d (ping=%v) got %v %v", t, grpcstate.Ping, err, res)
	if err != nil {
		log.Warnf("Error making grpc call: %v", err)
		grpcstate.RetCodes[-1]++
	} else {
		grpcstate.RetCodes[status]++
	}
}

// GRPCRunnerOptions includes the base RunnerOptions plus http specific
// options.
type GRPCRunnerOptions struct {
	periodic.RunnerOptions
	Destination        string
	Service            string        // Service to be checked when using grpc health check
	Profiler           string        // file to save profiles to. defaults to no profiling
	Payload            string        // Payload to be sent for grpc ping service
	Streams            int           // number of streams. total go routines and data streams will be streams*numthreads.
	Delay              time.Duration // Delay to be sent when using grpc ping service
	CACert             string        // Path to CA certificate for grpc TLS
	CertOverride       string        // Override the cert virtual host of authority for testing
	AllowInitialErrors bool          // whether initial errors don't cause an abort
	UsePing            bool          // use our own Ping proto for grpc load instead of standard health check one.
	UnixDomainSocket   string        // unix domain socket path to use for physical connection instead of Destination
}

// RunGRPCTest runs an http test and returns the aggregated stats.
func RunGRPCTest(o *GRPCRunnerOptions) (*GRPCRunnerResults, error) {
	if o.Streams < 1 {
		o.Streams = 1
	}
	if o.NumThreads < 1 {
		// sort of todo, this redoing some of periodic normalize (but we can't use normalize which does too much)
		o.NumThreads = periodic.DefaultRunnerOptions.NumThreads
	}
	if o.UsePing {
		o.RunType = "GRPC Ping"
		if o.Delay > 0 {
			o.RunType += fmt.Sprintf(" Delay=%v", o.Delay)
		}
	} else {
		o.RunType = "GRPC Health"
	}
	pll := len(o.Payload)
	if pll > 0 {
		o.RunType += fmt.Sprintf(" PayloadLength=%d", pll)
	}
	log.Infof("Starting %s test for %s with %d*%d threads at %.1f qps", o.RunType, o.Destination, o.Streams, o.NumThreads, o.QPS)
	o.NumThreads *= o.Streams
	r := periodic.NewPeriodicRunner(&o.RunnerOptions)
	defer r.Options().Abort()
	numThreads := r.Options().NumThreads // may change
	total := GRPCRunnerResults{
		RetCodes:    make(HealthResultMap),
		Destination: o.Destination,
		Streams:     o.Streams,
		Ping:        o.UsePing,
	}
	grpcstate := make([]GRPCRunnerResults, numThreads)
	out := r.Options().Out // Important as the default value is set from nil to stdout inside NewPeriodicRunner
	var conn *grpc.ClientConn
	var err error
	ts := time.Now().UnixNano()
	for i := 0; i < numThreads; i++ {
		r.Options().Runners[i] = &grpcstate[i]
		if (i % o.Streams) == 0 {
			conn, err = Dial(o)
			if err != nil {
				log.Errf("Error in grpc dial for %s %v", o.Destination, err)
				return nil, err
			}
		} else {
			log.Debugf("Reusing previous client connection for %d", i)
		}
		grpcstate[i].Ping = o.UsePing
		var err error
		if o.UsePing {
			grpcstate[i].clientP = NewPingServerClient(conn)
			if grpcstate[i].clientP == nil {
				return nil, fmt.Errorf("unable to create ping client %d for %s", i, o.Destination)
			}
			grpcstate[i].reqP = PingMessage{Payload: o.Payload, DelayNanos: o.Delay.Nanoseconds(), Seq: int64(i), Ts: ts}
			if o.Exactly <= 0 {
				_, err = grpcstate[i].clientP.Ping(context.Background(), &grpcstate[i].reqP)
			}
		} else {
			grpcstate[i].clientH = grpc_health_v1.NewHealthClient(conn)
			if grpcstate[i].clientH == nil {
				return nil, fmt.Errorf("unable to create health client %d for %s", i, o.Destination)
			}
			grpcstate[i].reqH = grpc_health_v1.HealthCheckRequest{Service: o.Service}
			if o.Exactly <= 0 {
				_, err = grpcstate[i].clientH.Check(context.Background(), &grpcstate[i].reqH)
			}
		}
		if !o.AllowInitialErrors && err != nil {
			log.Errf("Error in first grpc call (ping = %v) for %s: %v", o.UsePing, o.Destination, err)
			return nil, err
		}
		// Setup the stats for each 'thread'
		grpcstate[i].RetCodes = make(HealthResultMap)
	}

	if o.Profiler != "" {
		fc, err := os.Create(o.Profiler + ".cpu")
		if err != nil {
			log.Critf("Unable to create .cpu profile: %v", err)
			return nil, err
		}
		pprof.StartCPUProfile(fc) //nolint: gas,errcheck
	}
	total.RunnerResults = r.Run()
	if o.Profiler != "" {
		pprof.StopCPUProfile()
		fm, err := os.Create(o.Profiler + ".mem")
		if err != nil {
			log.Critf("Unable to create .mem profile: %v", err)
			return nil, err
		}
		runtime.GC()               // get up-to-date statistics
		pprof.WriteHeapProfile(fm) // nolint:gas,errcheck
		fm.Close()                 // nolint:gas,errcheck
		fmt.Printf("Wrote profile data to %s.{cpu|mem}\n", o.Profiler)
	}
	// Numthreads may have reduced
	numThreads = r.Options().NumThreads
	keys := []grpc_health_v1.HealthCheckResponse_ServingStatus{}
	for i := 0; i < numThreads; i++ {
		// Q: is there some copying each time stats[i] is used?
		for k := range grpcstate[i].RetCodes {
			if _, exists := total.RetCodes[k]; !exists {
				keys = append(keys, k)
			}
			total.RetCodes[k] += grpcstate[i].RetCodes[k]
		}
		// TODO: if grpc client needs 'cleanup'/Close like http one, do it on original NumThreads
	}
	// Cleanup state:
	r.Options().ReleaseRunners()
	which := "Health"
	if o.UsePing {
		which = "Ping"
	}
	for _, k := range keys {
		fmt.Fprintf(out, "%s %s : %d\n", which, k.String(), total.RetCodes[k])
	}
	return &total, nil
}

// grpcDestination parses dest and returns dest:port based on dest being
// a hostname, IP address, hostname:port, or ip:port. The original dest is
// returned if dest is an invalid hostname or invalid IP address. An http/https
// prefix is removed from dest if one exists and the port number is set to
// StandardHTTPPort for http, StandardHTTPSPort for https, or DefaultGRPCPort
// if http, https, or :port is not specified in dest.
// TODO: change/fix this (NormalizePort and more)
func grpcDestination(dest string) (parsedDest string) {
	var port string
	// strip any unintentional http/https scheme prefixes from dest
	// and set the port number.
	switch {
	case strings.HasPrefix(dest, fnet.PrefixHTTP):
		parsedDest = strings.TrimSuffix(strings.Replace(dest, fnet.PrefixHTTP, "", 1), "/")
		port = fnet.StandardHTTPPort
		log.Infof("stripping http scheme. grpc destination: %v: grpc port: %s",
			parsedDest, port)
	case strings.HasPrefix(dest, fnet.PrefixHTTPS):
		parsedDest = strings.TrimSuffix(strings.Replace(dest, fnet.PrefixHTTPS, "", 1), "/")
		port = fnet.StandardHTTPSPort
		log.Infof("stripping https scheme. grpc destination: %v. grpc port: %s",
			parsedDest, port)
	default:
		parsedDest = dest
		port = fnet.DefaultGRPCPort
	}
	if _, _, err := net.SplitHostPort(parsedDest); err == nil {
		return parsedDest
	}
	if ip := net.ParseIP(parsedDest); ip != nil {
		switch {
		case ip.To4() != nil:
			parsedDest = ip.String() + fnet.NormalizePort(port)
			return parsedDest
		case ip.To16() != nil:
			parsedDest = "[" + ip.String() + "]" + fnet.NormalizePort(port)
			return parsedDest
		}
	}
	// parsedDest is in the form of a domain name,
	// append ":port" and return.
	parsedDest += fnet.NormalizePort(port)
	return parsedDest
}
