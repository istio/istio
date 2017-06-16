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

package interfacegen

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"text/template"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"

	"istio.io/mixer/tools/codegen/pkg/modelgen"
)

// Generator generates Go interfaces for adapters to implement for a given Template.
type Generator struct {
	OutFilePath   string
	ImportMapping map[string]string
}

// Generate creates a Go interfaces for adapters to implement for a given Template.
func (g *Generator) Generate(fdsFile string) error {
	// This path works for bazel. TODO  Help pls !!

	tmplPath := "template/ProcInterface.tmpl"
	t, err := ioutil.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("cannot read template file '%s'. %v", tmplPath, err)
	}

	tmpl, err := template.New("ProcInterface").Parse(string(t))
	if err != nil {
		return fmt.Errorf("cannot load template from path '%s'. %v", tmplPath, err)
	}

	fds, err := getFileDescSet(fdsFile)
	if err != nil {
		return fmt.Errorf("cannot parse file '%s' as a FileDescriptorSetProto. %v", fdsFile, err)
	}

	parser, err := modelgen.CreateFileDescriptorSetParser(fds, g.ImportMapping)
	if err != nil {
		return fmt.Errorf("cannot parse file '%s' as a FileDescriptorSetProto. %v", fdsFile, err)
	}

	model, err := modelgen.Create(parser)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, model)
	if err != nil {
		return fmt.Errorf("cannot execute the template '%s' with the give data. %v", tmplPath, err)
	}

	// Now write to the file.
	if f, err := os.Create(g.OutFilePath); err != nil {
		return err
	} else if _, err = f.Write(buf.Bytes()); err != nil {
		return err
	} else {
		// file successfully written, close it.
		return f.Close()
	}
}

func getFileDescSet(path string) (*descriptor.FileDescriptorSet, error) {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fds := &descriptor.FileDescriptorSet{}
	err = proto.Unmarshal(bytes, fds)
	if err != nil {
		return nil, err
	}

	return fds, nil
}
