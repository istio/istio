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

package yml

import (
	"fmt"
	"io/ioutil"
	"os"

	"istio.io/istio/pkg/test"
)

type docType string

const (
	namespacesAndCRDs docType = "namespaces_and_crds"
	misc              docType = "misc"
)

// FileWriter write YAML content to files.
type FileWriter interface {
	// WriteYAML writes the given YAML content to one or more YAML files.
	WriteYAML(filenamePrefix string, contents ...string) ([]string, error)

	// WriteYAMLOrFail calls WriteYAML and fails the test if an error occurs.
	WriteYAMLOrFail(t test.Failer, filenamePrefix string, contents ...string) []string
}

type writerImpl struct {
	workDir string
}

// NewFileWriter creates a new FileWriter that stores files under workDir.
func NewFileWriter(workDir string) FileWriter {
	return &writerImpl{
		workDir: workDir,
	}
}

// WriteYAML writes the given YAML content to one or more YAML files.
func (w *writerImpl) WriteYAML(filenamePrefix string, contents ...string) ([]string, error) {
	out := make([]string, 0, len(contents))
	for _, content := range contents {
		files, err := splitContentsToFiles(w.workDir, content, filenamePrefix)
		if err != nil {
			return nil, err
		}

		if len(files) == 0 {
			f, err := writeContentsToTempFile(w.workDir, content)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
		}
		out = append(out, files...)
	}
	return out, nil
}

// WriteYAMLOrFial calls WriteYAML and fails the test if an error occurs.
func (w *writerImpl) WriteYAMLOrFail(t test.Failer, filenamePrefix string, contents ...string) []string {
	t.Helper()
	out, err := w.WriteYAML(filenamePrefix, contents...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeContentsToTempFile(workDir, contents string) (filename string, err error) {
	defer func() {
		if err != nil && filename != "" {
			_ = os.Remove(filename)
			filename = ""
		}
	}()

	var f *os.File
	f, err = ioutil.TempFile(workDir, "tmpyaml_")
	if err != nil {
		return
	}
	filename = f.Name()

	_, err = f.WriteString(contents)
	return
}

func splitContentsToFiles(workDir, content, filenamePrefix string) ([]string, error) {
	split := SplitYamlByKind(content)
	namespacesAndCrds := &yamlDoc{
		docType: namespacesAndCRDs,
		content: split["Namespace"],
	}
	misc := &yamlDoc{
		docType: misc,
		content: split["CustomResourceDefinition"],
	}

	// If all elements were put into a single doc just return an empty list, indicating that the original
	// content should be used.
	docs := []*yamlDoc{namespacesAndCrds, misc}
	for _, doc := range docs {
		if len(doc.content) == 0 {
			return make([]string, 0), nil
		}
	}

	filesToApply := make([]string, 0, len(docs))
	for _, doc := range docs {
		tfile, err := doc.toTempFile(workDir, filenamePrefix)
		if err != nil {
			return nil, err
		}
		filesToApply = append(filesToApply, tfile)
	}
	return filesToApply, nil
}

type yamlDoc struct {
	content string
	docType docType
}

func (d *yamlDoc) toTempFile(workDir, fileNamePrefix string) (string, error) {
	f, err := ioutil.TempFile(workDir, fmt.Sprintf("%s_%s.yaml", fileNamePrefix, d.docType))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	name := f.Name()

	_, err = f.WriteString(d.content)
	if err != nil {
		return "", err
	}
	return name, nil
}
