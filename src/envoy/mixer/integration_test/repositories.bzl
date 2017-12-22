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
load("@io_istio_istio//:x_tools_imports.bzl", "go_x_tools_imports_repositories")
load("@io_istio_istio//:istio_api.bzl", "go_istio_api_repositories")

def mixer_test_repositories(use_local_api=False):
    go_x_tools_imports_repositories()
    go_istio_api_repositories(use_local_api)

    native.git_repository(
        name = "org_pubref_rules_protobuf",
        commit = "563b674a2ce6650d459732932ea2bc98c9c9a9bf",  # Nov 28, 2017 (bazel 0.8.0 support)
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

    go_repository(
        name = "io_istio_gogo_genproto",
        commit = "09740ece0bc45a1cd0971a8b1f57c44b13ccd8dd",  # Dec 14, 2017 (initial generation of status protos)
        importpath = "istio.io/gogo-genproto",
    )

    go_repository(
        name = "org_uber_go_zap",
        commit = "9cabc84638b70e564c3dab2766efcb1ded2aac9f",  # Jun 8, 2017 (v1.4.1)
        importpath = "go.uber.org/zap",
    )

    go_repository(
        name = "org_uber_go_atomic",
        commit = "4e336646b2ef9fc6e47be8e21594178f98e5ebcf",  # Apr 12, 2017 (v1.2.0)
        importpath = "go.uber.org/atomic",
    )

    go_repository(
        name = "com_github_spf13_pflag",
        commit = "e57e3eeb33f795204c1ca35f56c44f83227c6e66",
        importpath = "github.com/spf13/pflag",
    )

    go_repository(
        name = "com_github_spf13_cobra",
        commit = "2df9a531813370438a4d79bfc33e21f58063ed87",
        importpath = "github.com/spf13/cobra",
    )
