package mock

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	pb "istio.io/api/security/v1alpha1"
)

// FakeExternalCA fake gRPC server for Istio Signing API
type FakeExternalCA struct {
	pb.UnimplementedIstioCertificateServiceServer
	g *grpc.Server
}

// NewFakeExternalCA create fake gRPC server for Istio Signing API
func NewFakeExternalCA() *FakeExternalCA {
	f := &FakeExternalCA{
		g: grpc.NewServer(),
	}
	pb.RegisterIstioCertificateServiceServer(f.g, f)
	return f
}

// Serve start listen on random port
func (f *FakeExternalCA) Serve() (net.Addr, error) {
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}

	go func() {
		err := f.g.Serve(lis)
		fmt.Println(fmt.Errorf("error on start Fake CA Server: %v", err))
	}()

	return lis.Addr(), nil
}

// Stop stop grpc server
func (f *FakeExternalCA) Stop() {
	f.g.Stop()
}

// CreateCertificate fake response cert chain
func (f *FakeExternalCA) CreateCertificate(ctx context.Context,
	req *pb.IstioCertificateRequest) (*pb.IstioCertificateResponse, error) {

	time.Sleep(2 * time.Second)

	return &pb.IstioCertificateResponse{
		CertChain: []string{"CERT", "ROOT_CERT"},
	}, nil
}
