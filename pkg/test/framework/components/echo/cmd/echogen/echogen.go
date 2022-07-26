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

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/kube"
	"istio.io/istio/pkg/test/framework/config"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/yml"
)

var (
	outputPath string
	dirOutput  bool
)

func init() {
	flag.StringVar(&outputPath, "out", "", "If specified, all generated output will be written to this file.")
	flag.BoolVar(&dirOutput, "dir", false, "If true, all generated output will be written to separate files per-config, in a directory named by -out.")
}

func main() {
	if !config.Parsed() {
		config.Parse()
	}
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}
	generate(flag.Args()[0], outputPath, dirOutput, os.Stdout)
}

func generate(input, output string, outputDir bool, outstream io.StringWriter) {
	if !config.Parsed() {
		// for tests
		config.Parse()
	}

	gen := newGenerator()
	if err := gen.load(input); err != nil {
		log.Fatalf("failed loading %s: %v", input, err)
	}
	if err := gen.generate(); err != nil {
		log.Fatalf("failed generating manifests: %v", err)
	}

	var err error
	if output != "" {
		err = gen.writeOutputFile(output, outputDir)
	} else {
		_, err = outstream.WriteString(gen.joinManifests())
	}
	if err != nil {
		log.Fatalf("failed writing output: %v", err)
	}
}

type generator struct {
	// settings
	settings *resource.Settings

	// internal
	configs   []echo.Config
	manifests map[string]string
}

func newGenerator() generator {
	// we read resource package settings to respsect --istio.test.versions
	settings, err := resource.SettingsFromCommandLine("echogen")
	if err != nil {
		log.Fatalf("failed reading test framework settings: %v", err)
	}

	return generator{
		settings: settings,
	}
}

func (g *generator) load(input string) error {
	// deserialize
	bytes, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("failed reading file: %v", err)
	}
	g.configs, err = echo.ParseConfigs(bytes)
	if err != nil {
		return fmt.Errorf("failed parsing file: %v", err)
	}
	// fill in defaults
	c := cluster.NewFake("fake", "1", "20")
	for i, cfg := range g.configs {
		if len(cfg.Ports) == 0 {
			cfg.Ports = ports.All()
		}
		cfg.Cluster = c
		if err := cfg.FillDefaults(nil); err != nil {
			return fmt.Errorf("failed filling defaults for %s: %v", cfg.ClusterLocalFQDN(), err)
		}
		g.configs[i] = cfg
	}
	return nil
}

func (g *generator) generate() error {
	outputByFQDN := map[string]string{}
	var errs error
	for _, cfg := range g.configs {
		id := cfg.ClusterLocalFQDN()
		// generate
		svc, err := kube.GenerateService(cfg)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed generating service for %s: %v", id, err))
			continue
		}
		deployment, err := kube.GenerateDeployment(nil, cfg, g.settings)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed generating deployment for %s: %v", id, err))
			continue
		}
		outputByFQDN[id] = yml.JoinString(svc, deployment)
		// add namespace if specified
		if cfg.Namespace.Name() != "" {
			var err error
			outputByFQDN[id], err = yml.ApplyNamespace(outputByFQDN[id], cfg.Namespace.Name())
			if err != nil {
				return fmt.Errorf("error applying namespace to %s: %v", id, err)
			}
		}

	}
	g.manifests = outputByFQDN
	return errs
}

func (g *generator) joinManifests() string {
	var m []string
	for _, yaml := range g.manifests {
		m = append(m, yaml)
	}
	return yml.JoinString(m...)
}

func (g *generator) writeOutputFile(path string, dir bool) error {
	if dir {
		// multi file
		if err := os.Mkdir(path, 0o644); err != nil {
			return fmt.Errorf("failed creating directory %s: %v", path, err)
		}
		for id, yaml := range g.manifests {
			fname := id + ".yaml"
			if err := os.WriteFile(fname, []byte(yaml), 0o644); err != nil {
				return fmt.Errorf("failed writing %s: %v", fname, err)
			}
		}
	} else if err := os.WriteFile(path, []byte(g.joinManifests()), 0o644); err != nil {
		return fmt.Errorf("failed writing %s: %v", path, err)
	}
	return nil
}
