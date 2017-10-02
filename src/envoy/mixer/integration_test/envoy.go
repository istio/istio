// Copyright 2017 Istio Authors. All Rights Reserved.
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

package test

import (
	"log"
	"os"
	"os/exec"
	"strings"
)

func getTestBinRootPath() string {
	switch {
	// custom path
	case os.Getenv("TEST_BIN_ROOT") != "":
		return os.Getenv("TEST_BIN_ROOT")
	// running under bazel
	case os.Getenv("TEST_SRCDIR") != "":
		return os.Getenv("TEST_SRCDIR") + "/__main__"
	// running with native go
	case os.Getenv("GOPATH") != "":
		list := strings.Split(os.Getenv("GOPATH"),
			string(os.PathListSeparator))
		return list[0] + "/bazel-bin"
	default:
		return "bazel-bin"
	}
}

type Envoy struct {
	cmd *exec.Cmd
}

// Run command and return the merged output from stderr and stdout, error code
func Run(name string, args ...string) (s string, err error) {
	log.Println(">", name, strings.Join(args, " "))
	c := exec.Command(name, args...)
	bytes, err := c.CombinedOutput()
	s = string(bytes)
	for _, line := range strings.Split(s, "\n") {
		log.Println(line)
	}
	if err != nil {
		log.Println(err)
	}
	return
}

func NewEnvoy(conf, flags string, stress, faultInject bool) (*Envoy, error) {
	bin_path := getTestBinRootPath() + "/src/envoy/mixer/envoy"
	log.Printf("Envoy binary: %v\n", bin_path)

	conf_path := "/tmp/envoy.conf"
	log.Printf("Envoy config: in %v\n%v\n", conf_path, conf)
	if err := CreateEnvoyConf(conf_path, conf, flags, stress, faultInject); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if stress {
		cmd = exec.Command(bin_path, "-c", conf_path, "--concurrency", "10")
	} else {
		cmd = exec.Command(bin_path, "-c", conf_path, "-l", "debug", "--concurrency", "1")
	}
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return &Envoy{
		cmd: cmd,
	}, nil
}

func (s *Envoy) Start() error {
	return s.cmd.Start()
}

func (s *Envoy) Stop() error {
	log.Printf("Kill Envoy ...\n")
	err := s.cmd.Process.Kill()
	log.Printf("Kill Envoy ... Done\n")
	return err
}
