# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
################################################################################
#

def go_x_tools_imports_repositories():
    BUILD_FILE = """
package(default_visibility = ["//visibility:public"])
load("@io_bazel_rules_go//go:def.bzl", "go_binary")
load("@io_bazel_rules_go//go:def.bzl", "go_prefix")

go_prefix("golang.org/x/tools")

licenses(["notice"])  # New BSD

exports_files(["LICENSE"])

go_binary(
    name = "goimports",
    srcs = [
        "cmd/goimports/doc.go",
        "cmd/goimports/goimports.go",
        "cmd/goimports/goimports_gc.go",
        "cmd/goimports/goimports_not_gc.go",
    ],
    deps = [
        "@org_golang_x_tools//imports:go_default_library",
    ],
)
"""
    # bazel rule for fixing up cfg.pb.go relies on running goimports
    # we import it here as a git repository to allow projection of a
    # simple build rule that will build the binary for usage (and avoid
    # the need to project a more complicated BUILD file over the entire
    # tools repo.)
    native.new_git_repository(
	name = "org_golang_x_tools_imports",
        build_file_content = BUILD_FILE,
        commit = "e6cb469339aef5b7be0c89de730d5f3cc8e47e50",  # Jun 23, 2017 (no releases)
        remote = "https://github.com/golang/tools.git",
    )
