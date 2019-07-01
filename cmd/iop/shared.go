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

func configLogs(args *rootArgs) error {
	opt := log.DefaultOptions()
	if !args.logToStdErr {
		opt.ErrorOutputPaths = []string{logFilePath}
		opt.OutputPaths = []string{logFilePath}
	}
	return log.Configure(opt)
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

	// Start with unmarshaling and validating the user CR (which is an overlay on the base profile).
	overlayICPS := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(overlayYAML, overlayICPS); err != nil {
		log.Fatalf(err.Error())
	}
	if errs := validate.CheckIstioControlPlaneSpec(overlayICPS); len(errs) != 0 {
		log.Fatalf(errs.ToError().Error())
	}

	// Now read the base profile specified in the user spec. If nothing specified, use default.
	baseYAML, err := helm.ReadValuesYAML(overlayICPS.Profile)
	if err != nil {
		log.Fatalf(err.Error())
	}
	// Unmarshal and validate the base CR.
	baseICPS := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(baseYAML, baseICPS); err != nil {
		log.Fatalf(err.Error())
	}
	if errs := validate.CheckIstioControlPlaneSpec(baseICPS); len(errs) != 0 {
		log.Fatalf(errs.ToError().Error())
	}

	mergedYAML, err := helm.OverlayYAML(baseYAML, overlayYAML)
	if err != nil {
		log.Fatalf(err.Error())
	}

	// Now unmarshal and validate the combined base profile and user CR overlay.
	mergedcps := &v1alpha2.IstioControlPlaneSpec{}
	if err := util.UnmarshalWithJSONPB(mergedYAML, mergedcps); err != nil {
		log.Fatalf(err.Error())
	}
	if errs := validate.CheckIstioControlPlaneSpec(mergedcps); len(errs) != 0 {
		log.Fatalf(errs.ToError().Error())
	}

	if yd := util.YAMLDiff(mergedYAML, util.ToYAMLWithJSONPB(mergedcps)); yd != "" {
		log.Fatalf("Validated YAML differs from input: \n%s", yd)
	}

	// TODO: remove version hard coding.
	cp := controlplane.NewIstioControlPlane(mergedcps, translate.Translators[version.NewMinorVersion(1, 2)])
	if err := cp.Run(); err != nil {
		log.Fatalf(err.Error())
	}

	manifests, errs := cp.RenderManifest()

	return manifests, errs.ToError()
}
