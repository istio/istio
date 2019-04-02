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

package testdata

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Load loads the test data set.
func Load() ([]*TestInfo, error) {
	infos := make(map[string]*TestInfo)

	// Go through each asset and constructs the test list by considering the input files only.
	// The other files are checked/verified based on the conventions.
	for _, asset := range AssetNames() {
		ext := path.Ext(asset)

		// Process .skip files and .yaml input files only.
		if ext != ".skip" && ext != ".yaml" {
			continue
		}

		baseFileName, idxSuffix, index, err := parseFileName(asset)
		if err != nil {
			return nil, err
		}

		// Skip if the asset is a meshconfig file. We will pick it up when processing the input file.
		if strings.HasSuffix(baseFileName, "_meshconfig") {
			continue
		}

		// Ensure that the info entry is generated.
		info, found := infos[baseFileName]
		if !found {
			testName := generateTestName(baseFileName)
			info = &TestInfo{
				testName: testName,
			}
			infos[baseFileName] = info
		}

		// If there is a "skip"" file, then mark the relevant test as ignored.
		if ext == ".skip" {
			info.Skipped = true
			continue
		}

		meshConfigFile := fmt.Sprintf("%s%s_meshconfig.yaml", baseFileName, idxSuffix)
		expectedFile := fmt.Sprintf("%s%s_expected.json", baseFileName, idxSuffix)

		// The expected file is required
		if _, err := AssetInfo(expectedFile); err != nil {
			return nil, err
		}

		// Mesh Config file is optional
		if _, err := AssetInfo(meshConfigFile); err != nil {
			meshConfigFile = ""
		}

		fs := &FileSet{
			inputFile:      asset,
			expectedFile:   expectedFile,
			meshConfigFile: meshConfigFile,
		}

		if len(info.files) <= index {
			arr := make([]*FileSet, index+1)
			copy(arr, info.files)
			info.files = arr
		}

		if info.files[index] != nil {
			return nil, fmt.Errorf("duplicate entry found for %q at index %d", baseFileName, index)
		}
		info.files[index] = fs
	}

	var result []*TestInfo

	// Do a sanity check to ensure there is data for each stage.
	for _, i := range infos {
		for j, fs := range i.files {
			if fs == nil {
				return nil, fmt.Errorf("missing stage '%d' for test %q", j, i.TestName())
			}
		}
		result = append(result, i)
	}

	// Order the infos, so that things can run  in a consistent order.
	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].TestName(), result[j].TestName()) < 0
	})

	return result, nil
}

func generateTestName(baseFileName string) string {
	testName := baseFileName
	testName = testName[strings.Index(testName, "/")+1:] // Strip dataset
	testName = strings.Replace(testName, "/", "_", -1)
	return testName
}

// parseFileName that is in the form foo/bar_1.yaml or foo/bar.yaml
// testName will be foo/bar.
// suffix will be either _1, _2 etc, or it will be empty if there is a single stage.
// stage is the stage number for the files. If no numeric suffix is defined, stage will default to 0.
func parseFileName(name string) (baseName, suffix string, stage int, err error) {
	// nameNoExtension is foo/bar_1 or foo/bar
	nameNoExtension := name[:len(name)-len(".yaml")]

	parts := strings.Split(nameNoExtension, "_")
	if len(parts) == 0 {
		return nameNoExtension, "", 0, nil
	}

	suffix = parts[len(parts)-1]
	i, err := strconv.ParseInt(suffix, 10, 32)
	if err != nil {
		return nameNoExtension, "", 0, nil
	}

	suffix = "_" + suffix
	baseName = nameNoExtension[0 : len(nameNoExtension)-len(suffix)]
	return baseName, suffix, int(i), nil
}
