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

package fortiogrpc

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"

	"istio.io/istio/devel/fortio"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// TODO: refactor common parts between http and grpc runners

// GrpcRunnerResults is the aggregated result of an GrpcRunner.
// Also is the internal type used per thread/goroutine.
type GrpcRunnerResults struct {
	fortio.RunnerResults
	client   grpc_health_v1.HealthClient
	req      grpc_health_v1.HealthCheckRequest
	RetCodes map[grpc_health_v1.HealthCheckResponse_ServingStatus]int64
}

// Used globally / in TestGrpc() TODO: change periodic.go to carry caller defined context
var (
	grpcstate []GrpcRunnerResults
)

// TestGrpc exercises Grpc health check at the target QPS.
// To be set as the Function in RunnerOptions.
func TestGrpc(t int) {
	fortio.Debugf("Calling in %d", t)
	res, err := grpcstate[t].client.Check(context.Background(), &grpcstate[t].req)
	fortio.Debugf("Got %v %v", res, err)
	if err != nil {
		fortio.Errf("Error making health check %v", err)
	} else {
		grpcstate[t].RetCodes[res.Status]++
	}
}

// GrpcRunnerOptions includes the base RunnerOptions plus http specific
// options.
type GrpcRunnerOptions struct {
	fortio.RunnerOptions
	Destination string
	Service     string
	Profiler    string // file to save profiles to. defaults to no profiling
}

// RunGrpcTest runs an http test and returns the aggregated stats.
func RunGrpcTest(o *GrpcRunnerOptions) (*GrpcRunnerResults, error) {
	// TODO lock
	if o.Function == nil {
		o.Function = TestGrpc
	}
	fortio.Infof("Starting grpc test for %s with %d threads at %.1f qps", o.Destination, o.NumThreads, o.QPS)
	r := fortio.NewPeriodicRunner(&o.RunnerOptions)
	numThreads := r.Options().NumThreads
	total := GrpcRunnerResults{
		RetCodes: make(map[grpc_health_v1.HealthCheckResponse_ServingStatus]int64),
	}
	grpcstate = make([]GrpcRunnerResults, numThreads)
	for i := 0; i < numThreads; i++ {
		// TODO: option to use certs
		conn, err := grpc.Dial(o.Destination, grpc.WithInsecure())
		if err != nil {
			fortio.Errf("Error in grpc dial for %s %v", o.Destination, err)
			return nil, err
		}
		grpcstate[i].client = grpc_health_v1.NewHealthClient(conn)
		if grpcstate[i].client == nil {
			return nil, fmt.Errorf("unable to create client %d for %s", i, o.Destination)
		}
		grpcstate[i].req = grpc_health_v1.HealthCheckRequest{Service: o.Service}
		_, err = grpcstate[i].client.Check(context.Background(), &grpcstate[i].req)
		if err != nil {
			fortio.Errf("Error in first grpc health check call for %s %v", o.Destination, err)
			return nil, err
		}
		// Setup the stats for each 'thread'
		grpcstate[i].RetCodes = make(map[grpc_health_v1.HealthCheckResponse_ServingStatus]int64)
	}

	if o.Profiler != "" {
		fc, err := os.Create(o.Profiler + ".cpu")
		if err != nil {
			fortio.Critf("Unable to create .cpu profile: %v", err)
			return nil, err
		}
		pprof.StartCPUProfile(fc) //nolint: gas,errcheck
	}
	total.RunnerResults = r.Run()
	if o.Profiler != "" {
		pprof.StopCPUProfile()
		fm, err := os.Create(o.Profiler + ".mem")
		if err != nil {
			fortio.Critf("Unable to create .mem profile: %v", err)
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
	}
	for _, k := range keys {
		fmt.Printf("Health %s : %d\n", k.String(), total.RetCodes[k])
	}
	return &total, nil
}
