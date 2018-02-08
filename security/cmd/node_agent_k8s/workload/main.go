package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/spf13/cobra"
	pb "istio.io/istio/security/proto"
)

const (
	sockFile string = "/tmp/udsver/nodeagent/server.sock"
)

var (
	cfgUdsFile string
	RootCmd    = &cobra.Command{
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
	defer conn.Close()

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
