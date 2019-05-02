// Copyright 2018 The Operator-SDK Authors
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

package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold/input"
	"github.com/operator-framework/operator-sdk/internal/util/k8sutil"
	"github.com/operator-framework/operator-sdk/internal/util/yamlutil"

	"github.com/coreos/go-semver/semver"
	"github.com/ghodss/yaml"
	olmapiv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
)

const (
	CSVYamlFileExt    = ".clusterserviceversion.yaml"
	CSVConfigYamlFile = "csv-config.yaml"
)

var ErrNoCSVVersion = errors.New("no CSV version supplied")

type CSV struct {
	input.Input

	// ConfigFilePath is the location of a configuration file path for this
	// projects' CSV file.
	ConfigFilePath string
	// CSVVersion is the CSV current version.
	CSVVersion string
	// FromVersion is the CSV version from which to build a new CSV. A CSV
	// manifest with this version should exist at:
	// deploy/olm-catalog/{from_version}/operator-name.v{from_version}.{CSVYamlFileExt}
	FromVersion string

	once       sync.Once
	fs         afero.Fs // For testing, ex. afero.NewMemMapFs()
	pathPrefix string   // For testing, ex. testdata/deploy/olm-catalog
}

func (s *CSV) initFS(fs afero.Fs) {
	s.once.Do(func() {
		s.fs = fs
	})
}

func (s *CSV) getFS() afero.Fs {
	s.initFS(afero.NewOsFs())
	return s.fs
}

func (s *CSV) GetInput() (input.Input, error) {
	// A CSV version is required.
	if s.CSVVersion == "" {
		return input.Input{}, ErrNoCSVVersion
	}
	if s.Path == "" {
		lowerProjName := strings.ToLower(s.ProjectName)
		// Path is what the operator-registry expects:
		// {manifests -> olm-catalog}/{operator_name}/{semver}/{operator_name}.v{semver}.clusterserviceversion.yaml
		s.Path = filepath.Join(s.pathPrefix,
			scaffold.OLMCatalogDir,
			lowerProjName,
			s.CSVVersion,
			getCSVFileName(lowerProjName, s.CSVVersion),
		)
	}
	if s.ConfigFilePath == "" {
		s.ConfigFilePath = filepath.Join(s.pathPrefix, scaffold.OLMCatalogDir, CSVConfigYamlFile)
	}
	return s.Input, nil
}

func (s *CSV) SetFS(fs afero.Fs) { s.initFS(fs) }

// CustomRender allows a CSV to be written by marshalling
// olmapiv1alpha1.ClusterServiceVersion instead of writing to a template.
func (s *CSV) CustomRender() ([]byte, error) {
	s.initFS(afero.NewOsFs())

	// Get current CSV to update.
	csv, exists, err := s.getBaseCSVIfExists()
	if err != nil {
		return nil, err
	}
	if !exists {
		csv = &olmapiv1alpha1.ClusterServiceVersion{}
		s.initCSVFields(csv)
	}

	cfg, err := GetCSVConfig(s.ConfigFilePath)
	if err != nil {
		return nil, err
	}

	setCSVDefaultFields(csv)
	if err = s.updateCSVVersions(csv); err != nil {
		return nil, err
	}
	if err = s.updateCSVFromManifestFiles(cfg, csv); err != nil {
		return nil, err
	}

	if fields := getEmptyRequiredCSVFields(csv); len(fields) != 0 {
		if exists {
			log.Warnf("Required csv fields not filled in file %s:%s\n", s.Path, joinFields(fields))
		} else {
			// A new csv won't have several required fields populated.
			// Report required fields to user informationally.
			log.Infof("Fill in the following required fields in file %s:%s\n", s.Path, joinFields(fields))
		}
	}

	return k8sutil.GetObjectBytes(csv)
}

func (s *CSV) getBaseCSVIfExists() (*olmapiv1alpha1.ClusterServiceVersion, bool, error) {
	verToGet := s.CSVVersion
	if s.FromVersion != "" {
		verToGet = s.FromVersion
	}
	csv, exists, err := getCSVFromFSIfExists(s.getFS(), s.getCSVPath(verToGet))
	if err != nil {
		return nil, false, err
	}
	if !exists && s.FromVersion != "" {
		log.Warnf("FromVersion set (%s) but CSV does not exist", s.FromVersion)
	}
	return csv, exists, nil
}

func getCSVFromFSIfExists(fs afero.Fs, path string) (*olmapiv1alpha1.ClusterServiceVersion, bool, error) {
	csvBytes, err := afero.ReadFile(fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(csvBytes) == 0 {
		return nil, false, nil
	}

	csv := &olmapiv1alpha1.ClusterServiceVersion{}
	if err := yaml.Unmarshal(csvBytes, csv); err != nil {
		return nil, false, err
	}

	return csv, true, nil
}

func getCSVName(name, version string) string {
	return name + ".v" + version
}

func getCSVFileName(name, version string) string {
	return getCSVName(name, version) + CSVYamlFileExt
}

func (s *CSV) getCSVPath(ver string) string {
	lowerProjName := strings.ToLower(s.ProjectName)
	name := getCSVFileName(lowerProjName, ver)
	return filepath.Join(s.pathPrefix, scaffold.OLMCatalogDir, lowerProjName, ver, name)
}

// getDisplayName turns a project dir name in any of {snake, chain, camel}
// cases, hierarchical dot structure, or space-delimited into a
// space-delimited, title'd display name.
// Ex. "another-_AppOperator_againTwiceThrice More"
// ->  "Another App Operator Again Twice Thrice More"
func getDisplayName(name string) string {
	for _, sep := range ".-_ " {
		splitName := strings.Split(name, string(sep))
		for i := 0; i < len(splitName); i++ {
			if splitName[i] == "" {
				splitName = append(splitName[:i], splitName[i+1:]...)
				i--
			} else {
				splitName[i] = strings.TrimSpace(splitName[i])
			}
		}
		name = strings.Join(splitName, " ")
	}
	splitName := strings.Split(name, " ")
	for i, word := range splitName {
		temp := word
		o := 0
		for j, r := range word {
			if unicode.IsUpper(r) {
				if j > 0 && !unicode.IsUpper(rune(word[j-1])) {
					temp = temp[0:j+o] + " " + temp[j+o:len(temp)]
					o++
				}
			}
		}
		splitName[i] = temp
	}
	return strings.TrimSpace(strings.Title(strings.Join(splitName, " ")))
}

// initCSVFields initializes all csv fields that should be populated by a user
// with sane defaults. initCSVFields should only be called for new csv's.
func (s *CSV) initCSVFields(csv *olmapiv1alpha1.ClusterServiceVersion) {
	// Metadata
	csv.TypeMeta.APIVersion = olmapiv1alpha1.ClusterServiceVersionAPIVersion
	csv.TypeMeta.Kind = olmapiv1alpha1.ClusterServiceVersionKind
	csv.SetName(getCSVName(strings.ToLower(s.ProjectName), s.CSVVersion))
	csv.SetNamespace("placeholder")
	csv.SetAnnotations(map[string]string{"capabilities": "Basic Install"})

	// Spec fields
	csv.Spec.Version = *semver.New(s.CSVVersion)
	csv.Spec.DisplayName = getDisplayName(s.ProjectName)
	csv.Spec.Description = "Placeholder description"
	csv.Spec.Maturity = "alpha"
	csv.Spec.Provider = olmapiv1alpha1.AppLink{}
	csv.Spec.Maintainers = make([]olmapiv1alpha1.Maintainer, 0)
	csv.Spec.Links = make([]olmapiv1alpha1.AppLink, 0)
}

// setCSVDefaultFields sets default fields on older CSV versions or newly
// initialized CSV's.
func setCSVDefaultFields(csv *olmapiv1alpha1.ClusterServiceVersion) {
	if len(csv.Spec.InstallModes) == 0 {
		csv.Spec.InstallModes = []olmapiv1alpha1.InstallMode{
			{Type: olmapiv1alpha1.InstallModeTypeOwnNamespace, Supported: true},
			{Type: olmapiv1alpha1.InstallModeTypeSingleNamespace, Supported: true},
			{Type: olmapiv1alpha1.InstallModeTypeMultiNamespace, Supported: false},
			{Type: olmapiv1alpha1.InstallModeTypeAllNamespaces, Supported: true},
		}
	}
}

// TODO: validate that all fields from files are populated as expected
// ex. add `resources` to a CRD

func getEmptyRequiredCSVFields(csv *olmapiv1alpha1.ClusterServiceVersion) (fields []string) {
	// Metadata
	if csv.TypeMeta.APIVersion != olmapiv1alpha1.ClusterServiceVersionAPIVersion {
		fields = append(fields, "apiVersion")
	}
	if csv.TypeMeta.Kind != olmapiv1alpha1.ClusterServiceVersionKind {
		fields = append(fields, "kind")
	}
	if csv.ObjectMeta.Name == "" {
		fields = append(fields, "metadata.name")
	}
	// Spec fields
	if csv.Spec.Version.String() == "" {
		fields = append(fields, "spec.version")
	}
	if csv.Spec.DisplayName == "" {
		fields = append(fields, "spec.displayName")
	}
	if csv.Spec.Description == "" {
		fields = append(fields, "spec.description")
	}
	if len(csv.Spec.Keywords) == 0 {
		fields = append(fields, "spec.keywords")
	}
	if len(csv.Spec.Maintainers) == 0 {
		fields = append(fields, "spec.maintainers")
	}
	if csv.Spec.Provider == (olmapiv1alpha1.AppLink{}) {
		fields = append(fields, "spec.provider")
	}
	if csv.Spec.Maturity == "" {
		fields = append(fields, "spec.maturity")
	}

	return fields
}

func joinFields(fields []string) string {
	sb := &strings.Builder{}
	for _, f := range fields {
		sb.WriteString("\n\t" + f)
	}
	return sb.String()
}

// updateCSVVersions updates csv's version and data involving the version,
// ex. ObjectMeta.Name, and place the old version in the `replaces` object,
// if there is an old version to replace.
func (s *CSV) updateCSVVersions(csv *olmapiv1alpha1.ClusterServiceVersion) error {

	// Old csv version to replace, and updated csv version.
	oldVer, newVer := csv.Spec.Version.String(), s.CSVVersion
	if oldVer == newVer {
		return nil
	}

	// We do not want to update versions in most fields, as these versions are
	// independent of global csv version and will be updated elsewhere.
	fieldsToUpdate := []interface{}{
		&csv.ObjectMeta,
		&csv.Spec.Labels,
		&csv.Spec.Selector,
	}
	for _, v := range fieldsToUpdate {
		err := replaceAllBytes(v, []byte(oldVer), []byte(newVer))
		if err != nil {
			return err
		}
	}

	// Now replace all references to the old operator name.
	lowerProjName := strings.ToLower(s.ProjectName)
	oldCSVName := getCSVName(lowerProjName, oldVer)
	newCSVName := getCSVName(lowerProjName, newVer)
	err := replaceAllBytes(csv, []byte(oldCSVName), []byte(newCSVName))
	if err != nil {
		return err
	}

	csv.Spec.Version = *semver.New(newVer)
	csv.Spec.Replaces = oldCSVName
	return nil
}

func replaceAllBytes(v interface{}, old, new []byte) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = bytes.Replace(b, old, new, -1)
	if err = json.Unmarshal(b, v); err != nil {
		return err
	}
	return nil
}

// updateCSVFromManifestFiles gathers relevant data from generated and
// user-defined manifests and updates csv.
func (s *CSV) updateCSVFromManifestFiles(cfg *CSVConfig, csv *olmapiv1alpha1.ClusterServiceVersion) error {
	store := NewUpdaterStore()
	otherSpecs := make(map[string][][]byte)
	for _, f := range append(cfg.CRDCRPaths, cfg.OperatorPath, cfg.RolePath) {
		yamlData, err := afero.ReadFile(s.getFS(), f)
		if err != nil {
			return err
		}

		scanner := yamlutil.NewYAMLScanner(yamlData)
		for scanner.Scan() {
			yamlSpec := scanner.Bytes()
			kind, err := getKindfromYAML(yamlSpec)
			if err != nil {
				return err
			}
			found, err := store.AddToUpdater(yamlSpec, kind)
			if err != nil {
				return err
			}
			if !found {
				if _, ok := otherSpecs[kind]; !ok {
					otherSpecs[kind] = make([][]byte, 0)
				}
				otherSpecs[kind] = append(otherSpecs[kind], yamlSpec)
			}
		}
		if err = scanner.Err(); err != nil {
			return err
		}
	}

	for k := range store.crds.crKinds {
		if crSpecs, ok := otherSpecs[k]; ok {
			for _, spec := range crSpecs {
				if err := store.AddCR(spec); err != nil {
					return err
				}
			}
		}
	}

	return store.Apply(csv)
}
