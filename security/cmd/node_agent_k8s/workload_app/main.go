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

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "istio.io/istio/security/proto"
)

const (
	sockFile string = "/tmp/udsver/nodeagent/server.sock"
)

var (
	cfgUdsFile string
	// RootCmd define the root command
	RootCmd = &cobra.Command{
		Use:   "udsverRpc",
		Short: "Test Grpc crediential verification over uds",
		Long:  "Test Grpc crediential verification over uds",
	}
)

func init() {
	RootCmd.PersistentFlags().StringVarP(&cfgUdsFile, "sock", "s", sockFile, "Unix domain socket file")
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	var conn *grpc.ClientConn
	var err error
	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure())
	opts = append(opts, grpc.WithDialer(unixDialer))
	fmt.Printf("start to connect with server")

	conn, err = grpc.Dial(sockFile, opts...)
	if err != nil {
		fmt.Printf("failed to connect with server %v", err)
	}
	defer conn.Close() // nolint: errcheck

	client := pb.NewWorkloadServiceClient(conn)
	for i := 0; i < 100; i++ {
		check(client)
		time.Sleep(1000 * time.Millisecond)
	}
}

func check(client pb.WorkloadServiceClient) {
	req := &pb.CheckRequest{Name: "foo"}
	resp, err := client.Check(context.Background(), req)
	if err != nil {
		fmt.Printf("%v.Check(_) = _, %v: ", client, err)
	}
	log.Println(resp)
}

func unixDialer(target string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", target, timeout)
}
