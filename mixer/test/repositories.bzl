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
load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "go_repository")
load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogo_proto_repositories")
load("@com_github_istio_mixer//:x_tools_imports.bzl", "go_x_tools_imports_repositories")
load("@com_github_istio_mixer//:googleapis.bzl", "go_googleapis_repositories")
load("@com_github_istio_mixer//:istio_api.bzl", "go_istio_api_repositories")


# This function should be used by others to use mock mixer.
# Before loading this bzl file, following repositoies should be loaded. 
#
# Usage:
#
# git_repository(
#     name = "io_bazel_rules_go",
#     commit = "7991b6353e468ba5e8403af382241d9ce031e571",  # Aug 1, 2017 (gazelle fixes)
#    remote = "https://github.com/bazelbuild/rules_go.git",
# )
#
# git_repository(
#     name = "org_pubref_rules_protobuf",
#     commit = "9ede1dbc38f0b89ae6cd8e206a22dd93cc1d5637",
#     remote = "https://github.com/pubref/rules_protobuf",
# )
#
# git_repository(
#     name = "com_github_istio_mixer",
#     commit = "4b3296a43ce940ba47fab7ad35fdf5c0c18778cd",
#     importpath = "github.com/istio/mixer",
# )
#
# load("@com_github_istio_mixer//test:repositories.bzl", "mixer_test_repositories")
# mixer_test_repositories(False)
#
def mixer_test_repositories(use_local_api=False):
    go_repositories()
    gogo_proto_repositories()
    go_x_tools_imports_repositories()
    go_istio_api_repositories(use_local_api)
    go_googleapis_repositories()

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
