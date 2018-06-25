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

package util

import (
	"fmt"
	"go/build"
	"os"
	"strings"

	"istio.io/istio/pkg/log"
)

var (
	// IstioTop has the top of the istio tree, matches the env variable from make.
	IstioTop = getIstioTop()

	// IstioSrc is the location if istio source ($TOP/src/istio.io/istio
	IstioSrc = getIstioSrc()

	// IstioBin is the location of the binary output directory
	IstioBin = getIstioBin()

	// IstioOut is the location of the output directory ($TOP/out)
	IstioOut = getIstioOut()
)

func getIstioTop() string {
	istioTop := os.Getenv("TOP")
	if istioTop == "" {
		// Assume it is run inside istio.io/istio
		current, _ := os.Getwd()
		idx := strings.Index(current, "/src/istio.io/istio")
		if idx > 0 {
			istioTop = current[0:idx]
		}
	}
	return istioTop
}

func getIstioSrc() string {
	istioSrc := os.Getenv("ISTIO_GO")
	if istioSrc == "" {
		istioSrc = getIstioTop() + "/src/istio.io/istio"
	}
	return istioSrc
}

func getIstioBin() string {
	istioBin := os.Getenv("ISTIO_BIN")
	if istioBin == "" {
		ctx := build.Default
		istioBin = fmt.Sprintf("%s/bin", ctx.GOPATH)
	}
	if !fileExists(istioBin) {
		log.Warnf("unable to resolve ISTIO_BIN. Dir %s does not exist", istioBin)
		istioBin = ""
	}
	return istioBin
}

func getIstioOut() string {
	istioOut := os.Getenv("ISTIO_OUT")
	if istioOut == "" {
		ctx := build.Default
		istioOut = fmt.Sprintf("%s/out/%s_%s", ctx.GOPATH, ctx.GOOS, ctx.GOARCH)
	}
	if !fileExists(istioOut) {
		log.Warnf("unable to resolve ISTIO_OUT. Dir %s does not exist", istioOut)
		istioOut = ""
	}
	return istioOut
}

func fileExists(f string) bool {
	_, err := os.Stat(f)
	return !os.IsNotExist(err)
}
