// Copyright Istio Authors
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

package perf

import (
	"fmt"
	"os"
	"path"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	testEnv "istio.io/istio/mixer/pkg/server"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/log"
)

type server struct {
	s *testEnv.Server
}

func (s *server) initialize(setup *Setup, settings *Settings) error {

	args, err := initializeArgs(settings, setup)
	if err != nil {
		return err
	}

	server, err := testEnv.New(args)
	if err != nil {
		return err
	}

	s.s = server

	s.s.Run()

	return nil
}

func initializeArgs(settings *Settings, setup *Setup) (*testEnv.Args, error) {
	serverDir, err := initializeServerDir(setup)
	if err != nil {
		return nil, err
	}

	templates := settings.Templates
	adapters := settings.Adapters

	if setup.Config.Templates != nil && len(setup.Config.Templates) > 0 {
		templates = make(map[string]template.Info)
		for _, name := range setup.Config.Templates {
			t, found := settings.findTemplate(name)
			if !found {
				return nil, fmt.Errorf("template not found: %s", name)
			}
			templates[t.Name] = t
		}
	}

	if setup.Config.Adapters != nil && len(setup.Config.Adapters) > 0 {
		adapters = make([]adapter.InfoFn, len(setup.Config.Adapters))
		for i, name := range setup.Config.Adapters {
			a, found := settings.findAdapter(name)
			if !found {
				return nil, fmt.Errorf("adapter not found: %s", name)
			}
			adapters[i] = a
		}
	}

	var args = testEnv.DefaultArgs()
	args.APIPort = 0
	args.MonitoringPort = 0
	args.Templates = templates
	args.Adapters = adapters
	args.ConfigStoreURL = `fs://` + serverDir
	args.ConfigDefaultNamespace = "istio-system"
	args.SingleThreaded = setup.Config.SingleThreaded

	if setup.Config.EnableDebugLog {
		// Override enableLog. This should skip the next if conditional
		setup.Config.EnableLog = true

		o := log.DefaultOptions()
		o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
		o.SetOutputLevel("adapters", log.DebugLevel)
		args.LoggingOptions = o
	}

	if !setup.Config.EnableLog {
		o := log.DefaultOptions()
		o.SetOutputLevel(log.DefaultScopeName, log.NoneLevel)
		o.SetOutputLevel("adapters", log.NoneLevel)
		args.LoggingOptions = o
	}

	args.LoggingOptions.LogGrpc = false // prevent race in grpclog.SetLogger

	return args, nil
}

func (s *server) shutdown() {
	if s.s != nil {
		if err := s.s.Close(); err != nil {
			log.Error(err.Error())
			_ = log.Sync()
		}
		s.s = nil
	}
}

func (s *server) address() string {
	return s.s.Addr().String()
}

func initializeServerDir(setup *Setup) (string, error) {
	t0 := time.Now()
	discriminator := fmt.Sprintf("%d-%d-%d-%d-%d-%d-%d",
		t0.Year(), t0.Month(), t0.Day(), t0.Hour(), t0.Minute(), t0.Second(), t0.Nanosecond())

	dir := path.Join(os.TempDir(), discriminator)

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", err
	}

	if err := write(path.Join(dir, "srvc.yaml"), []byte(setup.Config.Service)); err != nil {
		return "", err
	}

	if err := write(path.Join(dir, "global.yaml"), []byte(setup.Config.Global)); err != nil {
		return "", err
	}

	return dir, nil
}

func write(file string, bytes []byte) error {
	var f *os.File
	var err error

	if f, err = os.Create(file); err != nil {
		return err
	}

	if _, err = f.Write(bytes); err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}
