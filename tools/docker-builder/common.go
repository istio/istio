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

package main

import (
	"fmt"
	"strings"
)

func extractTags(a Args, target, variant string, hasDoubleDefault bool) []string {
	tags := []string{}
	for _, h := range a.Hubs {
		for _, tg := range a.Tags {
			if variant == DefaultVariant {
				// For default, we have no suffix
				tags = append(tags, fmt.Sprintf("%s/%s:%s", h, target, tg))
			} else {
				// Otherwise, we have a suffix with the variant
				tags = append(tags, fmt.Sprintf("%s/%s:%s-%s", h, target, tg, variant))
				// If we need a default as well, add it as a second tag for the same image to avoid building twice
				if variant == PrimaryVariant && hasDoubleDefault {
					tags = append(tags, fmt.Sprintf("%s/%s:%s", h, target, tg))
				}
			}
		}
	}
	return tags
}

func createArgs(args Args, target string, variant string, architecture string) map[string]string {
	baseDist := variant
	if baseDist == DefaultVariant {
		baseDist = PrimaryVariant
	}
	m := map[string]string{
		// Base version defines the tag of the base image to use. Typically, set in the Makefile and not overridden.
		"BASE_VERSION": args.BaseVersion,
		// Base distribution picks which variant to build
		"BASE_DISTRIBUTION": baseDist,
		// Additional metadata injected into some images
		"proxy_version":    args.ProxyVersion,
		"istio_version":    args.IstioVersion,
		"VM_IMAGE_NAME":    vmImageName(target),
		"VM_IMAGE_VERSION": vmImageVersion(target),
	}
	// Only needed for crane - buildx does it automagically
	if architecture != "" {
		os, arch, _ := strings.Cut(architecture, "/")
		m["TARGETARCH"] = arch
		m["TARGETOS"] = os
	}
	return m
}

func vmImageName(target string) string {
	if !strings.HasPrefix(target, "app_sidecar") {
		// Not a VM
		return ""
	}
	if strings.HasPrefix(target, "app_sidecar_base") {
		return strings.Split(target, "_")[3]
	}

	return strings.Split(target, "_")[2]
}

func vmImageVersion(target string) string {
	if !strings.HasPrefix(target, "app_sidecar") {
		// Not a VM
		return ""
	}
	if strings.HasPrefix(target, "app_sidecar_base") {
		return strings.Split(target, "_")[4]
	}

	return strings.Split(target, "_")[3]
}
