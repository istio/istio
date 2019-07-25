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

// Package iop contains types and functions that are used across the full
// set of mixer commands.
package iop

import (
	"fmt"
	"io/ioutil"
	"os"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/component/controlplane"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	"istio.io/operator/pkg/version"
	"istio.io/pkg/log"
)

const (
	logFilePath = "./iop.log"
)

func getWriter(args *rootArgs) (*os.File, error) {
	writer := os.Stdout
	if args.outFilename != "" {
		file, err := os.Create(args.outFilename)
		if err != nil {
			return nil, err
		}

		writer = file
	}
	return writer, nil
}

func configLogs(args *rootArgs, logOpts *log.Options) error {
	if !args.logToStdErr {
		logOpts.ErrorOutputPaths = []string{logFilePath}
		logOpts.OutputPaths = []string{logFilePath}
		defer log.Infof("Start running command: %v", os.Args)
	}
	return log.Configure(logOpts)
}

func genManifests(args *rootArgs) (name.ManifestMap, error) {
	overlayYAML := ""
	if args.inFilename != "" {
		b, err := ioutil.ReadFile(args.inFilename)
		if err != nil {
			log.Fatalf("Could not open input file: %s", err)
		}
		overlayYAML = string(b)
	}

	overlayFilenameLog := args.inFilename
	if overlayFilenameLog == "" {
		overlayFilenameLog = "[Empty Filename]"
	}

	// Start with unmarshaling and validating the user CR (which is an overlay on the base profile).
	overlayICPS := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(overlayYAML, overlayICPS); err != nil {
		return nil, fmt.Errorf("could not unmarshal the overlay YAML from file: %s, caused by: %v", overlayFilenameLog, err)
	}
	if errs := validate.CheckIstioControlPlaneSpec(overlayICPS, false); len(errs) != 0 {
		return nil, fmt.Errorf("overlay spec failed validation against IstioControlPlaneSpec: \n%v\n, caused by: %v", overlayICPS, errs.ToError())
	}

	baseProfileName := overlayICPS.Profile
	if baseProfileName == "" {
		baseProfileName = "[Builtin Profile]"
	}

	// Now read the base profile specified in the user spec. If nothing specified, use default.
	baseYAML, err := helm.ReadValuesYAML(overlayICPS.Profile)
	if err != nil {
		return nil, fmt.Errorf("error reading YAML from profile: %s, caused by: %v", baseProfileName, err)
	}
	// Unmarshal and validate the base CR.
	baseICPS := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(baseYAML, baseICPS); err != nil {
		return nil, fmt.Errorf("could not unmarshal the base YAML from profile: %s, caused by: %v", baseProfileName, err)
	}
	if errs := validate.CheckIstioControlPlaneSpec(baseICPS, true); len(errs) != 0 {
		return nil, fmt.Errorf("base spec failed validation against IstioControlPlaneSpec: \n%v\n, caused by: %v", baseICPS, err)
	}

	mergedYAML, err := helm.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		return nil, fmt.Errorf("failed to merge base YAML (%s) and overlay YAML (%s), caused by: %v", baseProfileName, overlayFilenameLog, err)
	}

	// Now unmarshal and validate the combined base profile and user CR overlay.
	mergedICPS := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(mergedYAML, mergedICPS); err != nil {
		return nil, fmt.Errorf("could not unmarshal the merged YAML: \n%s\n, caused by: %v", mergedYAML, err)
	}
	if errs := validate.CheckIstioControlPlaneSpec(mergedICPS, true); len(errs) != 0 {
		return nil, fmt.Errorf("merged spec failed validation against IstioControlPlaneSpec: \n%v\n, caused by: %v", mergedICPS, errs.ToError())
	}

	if yd := util.YAMLDiff(mergedYAML, util.ToYAMLWithJSONPB(mergedICPS)); yd != "" {
		return nil, fmt.Errorf("merged YAML differs from merged spec: \n%s", yd)
	}

	log.Infof("Start running Istio control plane.")

	// TODO: remove version hard coding.
	cp := controlplane.NewIstioControlPlane(mergedICPS, translate.Translators[version.NewMinorVersion(1, 2)])
	if err := cp.Run(); err != nil {
		return nil, fmt.Errorf("failed to run Istio control plane with spec: \n%v\n, caused by: %v", mergedICPS, err)
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		return manifests, errs.ToError()
	}
	return manifests, nil
}

// TODO: this really doesn't belong here. Figure out if it's generally needed and possibly move to istio.io/pkg/log.
func logAndPrintf(args *rootArgs, v ...interface{}) {
	s := fmt.Sprintf(v[0].(string), v[1:]...)
	if !args.logToStdErr {
		fmt.Println(s)
		log.Infof(s)
	}
}

func logAndFatalf(args *rootArgs, v ...interface{}) {
	logAndPrintf(args, v...)
	os.Exit(-1)
}
