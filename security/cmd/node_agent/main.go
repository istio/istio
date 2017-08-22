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

package main

import (
	"flag"

	"github.com/golang/glog"
	"istio.io/auth/cmd/node_agent/na"
)

var (
	naConfig na.Config
)

func init() {
	naConfig.ServiceIdentity = flag.String("service-identity", "",
		"Service Identity the node agent is managing")
	naConfig.ServiceIdentityOrg = flag.String("org", "Juju org", "organization for the cert")
	naConfig.RSAKeySize = flag.Int("key-size", 1024, "Size of generated private key")
	naConfig.NodeIdentityCertFile = flag.String("na-cert", "", "Node Agent identity cert file")
	naConfig.NodeIdentityPrivateKeyFile = flag.String("na-key", "",
		"Node identity private key file")
	naConfig.IstioCAAddress = flag.String("ca-address", "127.0.0.1",
		"Istio CA address")
	naConfig.ServiceIdentityDir = flag.String("cert-dir", "./", "Certificate directory")
	naConfig.RootCACertFile = flag.String("root-cert", "", "Root Certi file")
	naConfig.Env = flag.Int("env", na.ONPREM, "Node Environment : onprem | gcp")
}

func main() {
	flag.Parse()
	nodeAgent := na.NewNodeAgent(&naConfig)
	glog.Infof("Starting Node Agent")
	nodeAgent.Start()
}
