//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package local

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"go.uber.org/multierr"
	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/server"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/internal"
)

type deployedMixer struct {
	conn   *grpc.ClientConn
	client istio_mixer_v1.MixerClient

	args    *server.Args
	server  *server.Server
	workdir string
}

var _ environment.DeployedMixer = &deployedMixer{}
var _ internal.Configurable = &deployedMixer{}
var _ io.Closer = &deployedMixer{}

func newMixer(ctx *internal.TestContext) (*deployedMixer, error) {
	dir, err := ctx.CreateTmpDirectory("mixer")
	if err != nil {
		return nil, err
	}

	args := server.DefaultArgs()
	args.APIPort = 0
	args.MonitoringPort = 0
	args.ConfigStoreURL = fmt.Sprintf("fs://%s", dir)

	args.Templates = generatedTmplRepo.SupportedTmplInfo
	args.Adapters = adapter.Inventory()

	mi, err := server.New(args)
	if err != nil {
		return nil, err
	}

	go mi.Run()

	conn, err := grpc.Dial(mi.Addr().String(), grpc.WithInsecure())
	if err != nil {
		_ = mi.Close()
		return nil, err
	}

	client := istio_mixer_v1.NewMixerClient(conn)

	return &deployedMixer{
		conn:    conn,
		client:  client,
		args:    args,
		server:  mi,
		workdir: dir,
	}, nil
}

func (d *deployedMixer) Report(t testing.TB, attributes map[string]interface{}) {
	t.Helper()

	req := istio_mixer_v1.ReportRequest{
		Attributes: []istio_mixer_v1.CompressedAttributes{
			getAttrBag(attributes,
				d.args.ConfigIdentityAttribute,
				d.args.ConfigIdentityAttributeDomain)},
	}
	_, err := d.client.Report(context.Background(), &req)

	if err != nil {
		t.Fatalf("Error sending report: %v", err)
	}
}

func (d *deployedMixer) ApplyConfig(cfg string) error {
	file := path.Join(d.workdir, "config.yaml")
	err := ioutil.WriteFile(file, []byte(cfg), os.ModePerm)

	if err == nil {
		// TODO: Yes, this is horrible. We can use CtrlZ to create a full loop for handling config updates.
		time.Sleep(time.Second * 3)
	}

	return err
}

func (d *deployedMixer) Close() error {
	err := d.conn.Close()
	err = multierr.Append(err, d.server.Close())

	return err
}

func getAttrBag(attrs map[string]interface{}, identityAttr, identityAttrDomain string) istio_mixer_v1.CompressedAttributes {
	requestBag := attribute.GetMutableBag(nil)
	requestBag.Set(identityAttr, identityAttrDomain)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto istio_mixer_v1.CompressedAttributes
	requestBag.ToProto(&attrProto, nil, 0)
	return attrProto
}
