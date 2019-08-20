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

package conformance

import (
	"fmt"
	"io/ioutil"
	"path"
)

// Load test data from the given directory
func Load(baseDir string) ([]*Test, error) {
	return load(baseDir, "")
}

func load(dir, prefix string) ([]*Test, error) {
	isTest, err := isTestDir(dir)
	if err != nil {
		return nil, err
	}

	if isTest {
		t, err := loadTest(dir, prefix)
		if err != nil {
			return nil, fmt.Errorf("unable to load test: %v (dir: %q)", err, dir)
		}
		return []*Test{t}, nil
	}

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var result []*Test
	for _, f := range files {
		if !f.IsDir() {
			continue
		}

		d := path.Join(dir, f.Name())
		p := path.Join(prefix, f.Name())

		ts, err := load(d, p)
		if err != nil {
			return nil, err
		}

		result = append(result, ts...)
	}

	return result, nil
}

func isTestDir(dir string) (bool, error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return false, err
	}

	for _, f := range files {
		if f.Name() == MetadataFileName {
			return true, nil
		}
	}

	return false, nil
}

func loadTest(dir, name string) (*Test, error) {
	m, err := loadMetadata(dir, name)
	if err != nil {
		return nil, fmt.Errorf("unable to load metadata: %q", err)
	}

	staged, err := hasStages(dir)
	if err != nil {
		return nil, err
	}

	var stages []*Stage
	if !staged {
		stage, err := loadStage(dir)
		if err != nil {
			return nil, err
		}
		stages = []*Stage{stage}
	} else {
		entries, err := ioutil.ReadDir(dir)
		if err != nil {
			return nil, err
		}

		stMap := make(map[int]*Stage)
		for _, f := range entries {
			if !f.IsDir() {
				// There can be non-directory files in the case of staged execution. One example is the
				// test metadata file that spans all stages.
				continue
			}

			stageID, ok := parseStageName(f.Name())
			if !ok {
				return nil, fmt.Errorf("error while parsing the stage name of directory %q", f.Name())
			}

			stage, err := loadStage(path.Join(dir, f.Name()))
			if err != nil {
				return nil, fmt.Errorf("error in stage %d: %v", len(stages), err)
			}
			stMap[stageID] = stage
		}

		for i := 0; i < len(stMap); i++ {
			st, found := stMap[i]
			if !found {
				return nil, fmt.Errorf("stage not found: %d (test: %s)", i, m.Name)
			}
			stages = append(stages, st)
		}
	}

	return &Test{
		Metadata: m,
		Stages:   stages,
	}, nil
}
