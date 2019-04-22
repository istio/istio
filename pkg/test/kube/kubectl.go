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

package kube

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type kubectl struct {
	kubeConfig   string
	baseDir      string
	workDir      string
	workDirMutex sync.Mutex
}

// getWorkDir lazy-creates the working directory for the accessor.
func (c *kubectl) getWorkDir() (string, error) {
	c.workDirMutex.Lock()
	defer c.workDirMutex.Unlock()

	workDir := c.workDir
	if workDir == "" {
		var err error
		if workDir, err = ioutil.TempDir(c.baseDir, workDirPrefix); err != nil {
			return "", err
		}
		c.workDir = workDir
	}
	return workDir, nil
}

// applyContents applies the given config contents using kubectl.
func (c *kubectl) applyContents(namespace string, contents string) ([]string, error) {
	files, err := c.contentsToFileList(contents, "accessor_applyc")
	if err != nil {
		return nil, err
	}

	if err := c.applyInternal(namespace, files); err != nil {
		return nil, err
	}

	return files, nil
}

// apply the config in the given filename using kubectl.
func (c *kubectl) apply(namespace string, filename string) error {
	files, err := c.fileToFileList(filename)
	if err != nil {
		return err
	}

	return c.applyInternal(namespace, files)
}

func (c *kubectl) applyInternal(namespace string, files []string) error {
	for _, f := range files {
		command := fmt.Sprintf("kubectl apply %s %s -f %s", c.configArg(), namespaceArg(namespace), f)
		scopes.CI.Infof("Applying YAML: %s", command)
		s, err := shell.Execute(true, command)
		if err != nil {
			scopes.CI.Infof("(FAILED) Executing kubectl: %s (err: %v): %s", command, err, s)
			return fmt.Errorf("%v: %s", err, s)
		}
	}
	return nil
}

// deleteContents deletes the given config contents using kubectl.
func (c *kubectl) deleteContents(namespace, contents string) error {
	files, err := c.contentsToFileList(contents, "accessor_deletec")
	if err != nil {
		return err
	}

	return c.deleteInternal(namespace, files)
}

// delete the config in the given filename using kubectl.
func (c *kubectl) delete(namespace string, filename string) error {
	files, err := c.fileToFileList(filename)
	if err != nil {
		return err
	}

	return c.deleteInternal(namespace, files)
}

func (c *kubectl) deleteInternal(namespace string, files []string) (err error) {
	for i := len(files) - 1; i >= 0; i-- {
		scopes.CI.Infof("Deleting YAML file: %s", files[i])
		s, e := shell.Execute(true,
			"kubectl delete --ignore-not-found %s %s -f %s", c.configArg(), namespaceArg(namespace), files[i])
		if e != nil {
			return multierror.Append(err, fmt.Errorf("%v: %s", e, s))
		}
	}
	return
}

// logs calls the logs command for the specified pod, with -c, if container is specified.
func (c *kubectl) logs(namespace string, pod string, container string) (string, error) {
	cmd := fmt.Sprintf("kubectl logs %s %s %s %s",
		c.configArg(), namespaceArg(namespace), pod, containerArg(container))

	s, err := shell.Execute(true, cmd)

	if err == nil {
		return s, nil
	}

	return "", fmt.Errorf("%v: %s", err, s)
}

func (c *kubectl) exec(namespace, pod, container, command string) (string, error) {
	// Don't use combined output. The stderr and stdout streams are updated asynchronously and stderr can
	// corrupt the JSON output.
	return shell.Execute(false, "kubectl exec %s %s %s %s -- %s ",
		pod, namespaceArg(namespace), containerArg(container), c.configArg(), command)
}

func (c *kubectl) configArg() string {
	return configArg(c.kubeConfig)
}

func (c *kubectl) fileToFileList(filename string) ([]string, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	files, err := c.splitContentsToFiles(string(content), filenameWithoutExtension(filename))
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		files = append(files, filename)
	}

	return files, nil
}

func (c *kubectl) contentsToFileList(contents, filenamePrefix string) ([]string, error) {
	files, err := c.splitContentsToFiles(contents, filenamePrefix)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		f, err := c.writeContentsToTempFile(contents)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func (c *kubectl) writeContentsToTempFile(contents string) (filename string, err error) {
	defer func() {
		if err != nil && filename != "" {
			_ = os.Remove(filename)
			filename = ""
		}
	}()

	var workdir string
	workdir, err = c.getWorkDir()
	if err != nil {
		return
	}

	var f *os.File
	f, err = ioutil.TempFile(workdir, "accessor_")
	if err != nil {
		return
	}
	filename = f.Name()

	_, err = f.WriteString(contents)
	return
}

func (c *kubectl) splitContentsToFiles(content, filenamePrefix string) ([]string, error) {
	cfgs := test.SplitConfigs(content)

	namespacesAndCrds := &yamlDoc{
		docType: namespacesAndCRDs,
	}
	misc := &yamlDoc{
		docType: misc,
	}
	for _, cfg := range cfgs {
		var typeMeta kubeApiMeta.TypeMeta
		if e := yaml.Unmarshal([]byte(cfg), &typeMeta); e != nil {
			// Ignore invalid parts. This most commonly happens when it's empty or contains only comments.
			continue
		}

		switch typeMeta.Kind {
		case "Namespace":
			namespacesAndCrds.append(cfg)
		case "CustomResourceDefinition":
			namespacesAndCrds.append(cfg)
		default:
			misc.append(cfg)
		}
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
		workDir, err := c.getWorkDir()
		if err != nil {
			return nil, err
		}

		tfile, err := doc.toTempFile(workDir, filenamePrefix)
		if err != nil {
			return nil, err
		}
		filesToApply = append(filesToApply, tfile)
	}
	return filesToApply, nil
}

func configArg(kubeConfig string) string {
	if kubeConfig != "" {
		return fmt.Sprintf("--kubeconfig=%s", kubeConfig)
	}
	return ""
}

func namespaceArg(namespace string) string {
	if namespace != "" {
		return fmt.Sprintf("-n %s", namespace)
	}
	return ""
}

func containerArg(container string) string {
	if container != "" {
		return fmt.Sprintf("-c %s", container)
	}
	return ""
}

func filenameWithoutExtension(fullPath string) string {
	_, f := filepath.Split(fullPath)
	return strings.TrimSuffix(f, filepath.Ext(fullPath))
}

type docType string

const (
	namespacesAndCRDs docType = "namespaces_and_crds"
	misc              docType = "misc"
)

type yamlDoc struct {
	content string
	docType docType
}

func (d *yamlDoc) append(c string) {
	d.content = test.JoinConfigs(d.content, c)
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
