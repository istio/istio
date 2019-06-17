package util

import (
	"fmt"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/hashicorp/go-multierror"
)

func GetServerInfo(localHostAddr string, adminPort uint16) (*admin.ServerInfo, error) {
	input, err := doHTTPGet(fmt.Sprintf("http://%s:%d/server_info", localHostAddr, adminPort))
	if err != nil {
		return nil, multierror.Prefix(err, "failed retrieving Envoy stats:")
	}
	info := &admin.ServerInfo{}
	if err := jsonpb.Unmarshal(input, info); err != nil {
		return &admin.ServerInfo{}, err
	}

	return info, nil
}
