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
load("@io_bazel_rules_go//go:def.bzl", "go_repository")
load("@io_istio_istio//mixer:x_tools_imports.bzl", "go_x_tools_imports_repositories")
load("@io_istio_istio//mixer:istio_api.bzl", "go_istio_api_repositories")

# This function should be used by others to use mock mixer.
# Before loading this bzl file, following repositoies should be loaded.
#
# Usage:
#
# git_repository(
#     name = "io_bazel_rules_go",
#     commit = "9cf23e2aab101f86e4f51d8c5e0f14c012c2161c",  # Oct 12, 2017 (Add `build_external` option to `go_repository`)
#     remote = "https://github.com/bazelbuild/rules_go.git",
# )
#
# load("@io_bazel_rules_go//go:def.bzl", "go_rules_dependencies", "go_register_toolchains")
# go_rules_dependencies()
# go_register_toolchains()
#
# load("@io_bazel_rules_go//proto:def.bzl", "proto_register_toolchains")
# proto_register_toolchains()
#
# git_repository(
#     name = "io_istio_istio",
#     commit = "4b3296a43ce940ba47fab7ad35fdf5c0c18778cd",
#     importpath = "github.com/istio/istio",
# )
#
# load("@io_istio_istio//mixer/test:repositories.bzl", "mixer_test_repositories")
# mixer_test_repositories(False)
#
def mixer_test_repositories(use_local_api=False):
    go_x_tools_imports_repositories()
    go_istio_api_repositories(use_local_api)

    native.git_repository(
        name = "org_pubref_rules_protobuf",
        commit = "ff3b7e7963daa7cb3b42f8936bc11eda4b960926",  # Oct 03, 2017 (Updating External Import Paths)
        remote = "https://github.com/pubref/rules_protobuf",
    )

    go_repository(
        name = "com_github_gogo_protobuf",
        commit = "100ba4e885062801d56799d78530b73b178a78f3",  # Mar 7, 2017 (match pubref dep)
        importpath = "github.com/gogo/protobuf",
        build_file_proto_mode = "legacy",
    )

    go_repository(
        name = "org_golang_x_text",
        build_file_name = "BUILD.bazel",
        commit = "f4b4367115ec2de254587813edaa901bc1c723a8",  # Mar 31, 2017 (no releases)
        importpath = "golang.org/x/text",
    )

    go_repository(
        name = "org_golang_x_tools",
        commit = "e6cb469339aef5b7be0c89de730d5f3cc8e47e50",  # Jun 23, 2017 (no releases)
        importpath = "golang.org/x/tools",
    )

    go_repository(
        name = "com_github_hashicorp_go_multierror",
        commit = "ed905158d87462226a13fe39ddf685ea65f1c11f",  # Dec 16, 2016 (no releases)
        importpath = "github.com/hashicorp/go-multierror",
    )

    go_repository(
        name = "com_github_hashicorp_errwrap",
        commit = "7554cd9344cec97297fa6649b055a8c98c2a1e55",  # Oct 27, 2014 (no releases)
        importpath = "github.com/hashicorp/errwrap",
    )
