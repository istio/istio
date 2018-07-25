package skywalking

import (
	"testing"
	"google.golang.org/grpc"
	"fmt"
	pb "istio.io/istio/mixer/adapter/skywalking/protocol"
	"time"
	"context"
)

func TestGRPC(t *testing.T) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	conn, err := grpc.Dial("localhost:11800", opts...)

	if err != nil {
		fmt.Printf("fail to dial: %v", err)
	}

	client := pb.NewServiceMeshMetricServiceClient(conn)

	fmt.Printf("Begin to create client")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientStream, err := client.Collect(ctx)
	if err != nil {
		fmt.Printf("%v.Collect(_) = _, %v", client, err)
	}


	clientStream.Send(&pb.ServiceMeshMetric{})
	clientStream.CloseAndRecv()

	fmt.Printf("Metric sent")
}
