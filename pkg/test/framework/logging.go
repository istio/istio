//  Copyright Istio Authors
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

package framework

import (
	"flag"
	"io/ioutil"

	"google.golang.org/grpc/grpclog"

	"istio.io/pkg/log"
)

var (
	logOptionsFromCommandline = log.DefaultOptions()
)

func init() {
	logOptionsFromCommandline.AttachFlags(
		func(p *[]string, name string, value []string, usage string) {
			// TODO(ozben): Implement string array method for capturing the complete set of log settings.
		},
		flag.StringVar,
		flag.IntVar,
		flag.BoolVar)
}

func configureLogging() error {
	o := *logOptionsFromCommandline

	o.LogGrpc = false
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))

	return log.Configure(&o)
}
