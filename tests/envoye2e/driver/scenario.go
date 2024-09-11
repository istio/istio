// Copyright 2019 Istio Authors
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

package driver

import (
	"bytes"
	"fmt"
	"log"
	"reflect"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/golang/protobuf/jsonpb" // nolint: depguard // We need the deprecated module since the jsonpb replacement is not backwards compatible.
	// nolint: staticcheck
	legacyproto "github.com/golang/protobuf/proto"
	yamlv2 "gopkg.in/yaml.v2"
	"sigs.k8s.io/yaml"

	"istio.io/istio/tests/envoye2e/env"
)

type (
	// Params include test context that is shared by all steps.
	Params struct {
		// Config is the XDS server state.
		Config XDSServer

		// Ports record the port assignment for a test.
		Ports *env.Ports

		// Vars include the variables which are used to fill in configuration template files.
		Vars map[string]string

		// N records the index of repetition. It is only valid when using with Repeat step.
		N int
	}

	// Step is a unit of execution in the integration test.
	Step interface {
		// Run wraps the logic of a test step.
		Run(*Params) error

		// Cleanup cleans up all the test artifacts created by Run.
		Cleanup()
	}

	// Scenario is a collection of Steps. It runs and cleans up all steps sequentially.
	Scenario struct {
		// Steps is a collection of steps which be executed sequentially.
		Steps []Step
	}

	// Repeat a step either for N number or duration
	Repeat struct {
		N        int
		Duration time.Duration
		Step     Step
	}

	// Sleep injects a sleep with the given duration into the test execution.
	Sleep struct {
		time.Duration
	}

	// Fork will copy params to avoid concurrent access
	Fork struct {
		Fore Step
		Back Step
	}

	// StepFunction models Lambda captured step
	StepFunction func(p *Params) error
)

// NewTestParams creates a new test params struct which keeps state of a test. vars will be used for template filling.
// A set of ports will be assigned. If TestInventory is provided, the port assignment will be offsetted based on the index of the test in the inventory.
func NewTestParams(t *testing.T, vars map[string]string, inv *env.TestInventory) *Params {
	ind := inv.GetTestIndex(t)
	ports := env.NewPorts(ind)
	return &Params{
		Vars:  vars,
		Ports: ports,
	}
}

var _ Step = &Repeat{}

func (r *Repeat) Run(p *Params) error {
	if r.Duration != 0 {
		start := time.Now()
		p.N = 0
		for {
			if time.Since(start) >= r.Duration {
				break
			}
			log.Printf("repeat %d elapsed %v out of %v", p.N, time.Since(start), r.Duration)
			if err := r.Step.Run(p); err != nil {
				return err
			}
			p.N++
		}
	} else {
		for i := 0; i < r.N; i++ {
			log.Printf("repeat %d out of %d", i, r.N)
			p.N = i
			if err := r.Step.Run(p); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *Repeat) Cleanup() {}

var _ Step = &Sleep{}

func (s *Sleep) Run(_ *Params) error {
	log.Printf("sleeping %v\n", s.Duration)
	time.Sleep(s.Duration)
	return nil
}
func (s *Sleep) Cleanup() {}

// Fill a template file with ariable map in Params.
func (p *Params) Fill(s string) (string, error) {
	t := template.Must(template.New("params").
		Option("missingkey=zero").
		Funcs(template.FuncMap{
			"indent": func(n int, s string) string {
				pad := strings.Repeat(" ", n)
				return pad + strings.Replace(s, "\n", "\n"+pad, -1)
			},
			"fill": func(s string) string {
				out, err := p.Fill(s)
				if err != nil {
					panic(err)
				}
				return out
			},
			"divisible": func(i, j int) bool {
				return i%j == 0
			},
		}).
		Parse(s))
	var b bytes.Buffer
	if err := t.Execute(&b, p); err != nil {
		return "", err
	}
	return b.String(), nil
}

var _ Step = &Fork{}

func (f *Fork) Run(p *Params) error {
	done := make(chan error, 1)
	go func() {
		p2 := *p
		done <- f.Back.Run(&p2)
	}()

	if err := f.Fore.Run(p); err != nil {
		return err
	}

	return <-done
}
func (f *Fork) Cleanup() {}

func (s StepFunction) Run(p *Params) error {
	return s(p)
}

func (s StepFunction) Cleanup() {}

var _ Step = &Scenario{}

func (s *Scenario) Run(p *Params) error {
	passed := make([]Step, 0, len(s.Steps))
	defer func() {
		for i := range passed {
			passed[len(passed)-1-i].Cleanup()
		}
	}()
	for _, step := range s.Steps {
		if err := step.Run(p); err != nil {
			return err
		}
		passed = append(passed, step)
	}
	return nil
}

func (s *Scenario) Cleanup() {}

func ReadYAML(input string, pb legacyproto.Message) error {
	var jsonObj interface{}
	err := yamlv2.Unmarshal([]byte(input), &jsonObj)
	if err != nil {
		return fmt.Errorf("cannot parse: %v for %q", err, input)
	}
	// As a special case, convert [x] to x.
	// This is needed because jsonpb is unable to parse arrays.
	in := reflect.ValueOf(jsonObj)
	switch in.Kind() {
	case reflect.Slice, reflect.Array:
		if in.Len() == 1 {
			jsonObj = in.Index(0).Interface()
		}
	}
	yml, err := yamlv2.Marshal(jsonObj)
	if err != nil {
		return fmt.Errorf("cannot marshal: %v for %q", err, input)
	}
	js, err := yaml.YAMLToJSON(yml)
	if err != nil {
		return fmt.Errorf("cannot unmarshal: %v for %q", err, input)
	}
	reader := strings.NewReader(string(js))
	m := jsonpb.Unmarshaler{}
	return m.Unmarshal(reader, pb)
}

func (p *Params) FillYAML(input string, pb legacyproto.Message) error {
	out, err := p.Fill(input)
	if err != nil {
		return err
	}
	return ReadYAML(out, pb)
}
