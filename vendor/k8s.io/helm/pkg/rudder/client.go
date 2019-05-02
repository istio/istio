/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rudder // import "k8s.io/helm/pkg/rudder"

import (
	"fmt"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	rudderAPI "k8s.io/helm/pkg/proto/hapi/rudder"
)

// GrpcPort specifies port on which rudder will spawn a server
const (
	GrpcPort = 10001
)

var grpcAddr = fmt.Sprintf("127.0.0.1:%d", GrpcPort)

// InstallRelease calls Rudder InstallRelease method which should create provided release
func InstallRelease(rel *rudderAPI.InstallReleaseRequest) (*rudderAPI.InstallReleaseResponse, error) {
	//TODO(mkwiek): parametrize this
	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	defer conn.Close()
	client := rudderAPI.NewReleaseModuleServiceClient(conn)
	return client.InstallRelease(context.Background(), rel)
}

// UpgradeRelease calls Rudder UpgradeRelease method which should perform update
func UpgradeRelease(req *rudderAPI.UpgradeReleaseRequest) (*rudderAPI.UpgradeReleaseResponse, error) {
	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	client := rudderAPI.NewReleaseModuleServiceClient(conn)
	return client.UpgradeRelease(context.Background(), req)
}

// RollbackRelease calls Rudder RollbackRelease method which should perform update
func RollbackRelease(req *rudderAPI.RollbackReleaseRequest) (*rudderAPI.RollbackReleaseResponse, error) {
	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	client := rudderAPI.NewReleaseModuleServiceClient(conn)
	return client.RollbackRelease(context.Background(), req)
}

// ReleaseStatus calls Rudder ReleaseStatus method which should perform update
func ReleaseStatus(req *rudderAPI.ReleaseStatusRequest) (*rudderAPI.ReleaseStatusResponse, error) {
	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	client := rudderAPI.NewReleaseModuleServiceClient(conn)
	return client.ReleaseStatus(context.Background(), req)
}

// DeleteRelease calls Rudder DeleteRelease method which should uninstall provided release
func DeleteRelease(rel *rudderAPI.DeleteReleaseRequest) (*rudderAPI.DeleteReleaseResponse, error) {
	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	defer conn.Close()
	client := rudderAPI.NewReleaseModuleServiceClient(conn)
	return client.DeleteRelease(context.Background(), rel)
}
