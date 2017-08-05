# Copyright 2016 Google Inc. All Rights Reserved.
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

load(
    "//src/envoy/mixer:repositories.bzl",
    "mixer_client_repositories",
)

mixer_client_repositories()

load(
    "@mixerclient_git//:repositories.bzl",
    "googleapis_repositories",
    "mixerapi_repositories",
)

googleapis_repositories()
mixerapi_repositories()

bind(
    name = "boringssl_crypto",
    actual = "//external:ssl",
)

git_repository(
    name = "envoy",
    remote = "https://github.com/lyft/envoy.git",
    commit = "a2a47b2e8072126b0f1d4128eca13759d9ae2155",
)

load("@envoy//bazel:repositories.bzl", "envoy_dependencies")

envoy_dependencies()

load("@envoy//bazel:cc_configure.bzl", "cc_configure")

cc_configure()

load("@envoy_api//bazel:repositories.bzl", "api_dependencies")

api_dependencies()

# Following go repositories are for building go integration test for mixer filter.
git_repository(
    name = "io_bazel_rules_go",
    commit = "87cdda3fc0fd65c63ef0316533be03ea4956f809",  # April 7 2017 (0.4.2)
    remote = "https://github.com/bazelbuild/rules_go.git",
)

git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "d8624842ab237a3f9fecbbe4fae1a33ae3215d2e",
    # forked with special fix: https://github.com/pubref/rules_protobuf/pull/93
    remote = "https://github.com/qiwzhang/rules_protobuf",
)

###########################################
# Following dependencies are needed for building istio/mixer for
# //src/envoy/mixer/integration_test
# They are directly copied from its WORKSPACE

load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "new_go_repository")
go_repositories()

load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_repositories")
proto_repositories()

load("@org_pubref_rules_protobuf//cpp:rules.bzl", "cpp_proto_repositories")
cpp_proto_repositories()

load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogo_proto_repositories")
gogo_proto_repositories()

new_go_repository(
    name = "com_github_golang_glog",
    commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",  # Jan 26, 2016 (no releases)
    importpath = "github.com/golang/glog",
)

new_go_repository(
    name = "com_github_ghodss_yaml",
    commit = "04f313413ffd65ce25f2541bfd2b2ceec5c0908c",  # Dec 6, 2016 (no releases)
    importpath = "github.com/ghodss/yaml",
)

new_go_repository(
    name = "in_gopkg_yaml_v2",
    commit = "14227de293ca979cf205cd88769fe71ed96a97e2",  # Jan 24, 2017 (no releases)
    importpath = "gopkg.in/yaml.v2",
)

new_go_repository(
    name = "com_github_golang_protobuf",
    commit = "8ee79997227bf9b34611aee7946ae64735e6fd93",  # Nov 16, 2016 (no releases)
    importpath = "github.com/golang/protobuf",
)

new_go_repository(
    name = "org_golang_google_grpc",
    commit = "cdee119ee21e61eef7093a41ba148fa83585e143",  # Mar 14, 2017 (v1.2.0)
    importpath = "google.golang.org/grpc",
)

new_go_repository(
    name = "com_github_spf13_cobra",
    commit = "35136c09d8da66b901337c6e86fd8e88a1a255bd",  # Jan 30, 2017 (no releases)
    importpath = "github.com/spf13/cobra",
)

new_go_repository(
    name = "com_github_spf13_pflag",
    commit = "9ff6c6923cfffbcd502984b8e0c80539a94968b7",  # Jan 30, 2017 (no releases)
    importpath = "github.com/spf13/pflag",
)

new_go_repository(
    name = "com_github_cpuguy83_go_md2man",
    commit = "648eed146d3f3beacb64063cd0daae908015eebd",  # Mar 19, 2017 (no releases)
    importpath = "github.com/cpuguy83/go-md2man",
)

new_go_repository(
    name = "com_github_russross_blackfriday",
    commit = "35eb537633d9950afc8ae7bdf0edb6134584e9fc",  # Mar 19, 2017 (no releases)
    importpath = "github.com/russross/blackfriday",
)

new_go_repository(
    name = "com_github_shurcooL_sanitized_anchor_name",
    commit = "10ef21a441db47d8b13ebcc5fd2310f636973c77",  # Mar 19, 2017 (no releases)
    importpath = "github.com/shurcooL/sanitized_anchor_name",
)

new_go_repository(
    name = "com_github_hashicorp_go_multierror",
    commit = "ed905158d87462226a13fe39ddf685ea65f1c11f",  # Dec 16, 2016 (no releases)
    importpath = "github.com/hashicorp/go-multierror",
)

new_go_repository(
    name = "com_github_hashicorp_errwrap",
    commit = "7554cd9344cec97297fa6649b055a8c98c2a1e55",  # Oct 27, 2014 (no releases)
    importpath = "github.com/hashicorp/errwrap",
)

new_go_repository(
    name = "com_github_opentracing_opentracing_go",
    commit = "0c3154a3c2ce79d3271985848659870599dfb77c",  # Sep 26, 2016 (v1.0.0)
    importpath = "github.com/opentracing/opentracing-go",
)

new_go_repository(
    name = "com_github_opentracing_basictracer",
    commit = "1b32af207119a14b1b231d451df3ed04a72efebf",  # Sep 29, 2016 (no releases)
    importpath = "github.com/opentracing/basictracer-go",
)

new_go_repository(
    name = "com_github_prometheus_client_golang",
    commit = "c5b7fccd204277076155f10851dad72b76a49317",  # Aug 17, 2016 (v0.8.0)
    importpath = "github.com/prometheus/client_golang",
)

new_go_repository(
    name = "com_github_prometheus_common",
    commit = "dd2f054febf4a6c00f2343686efb775948a8bff4",  # Jan 8, 2017 (no releases)
    importpath = "github.com/prometheus/common",
)

new_go_repository(
    name = "com_github_matttproud_golang_protobuf_extensions",
    commit = "c12348ce28de40eed0136aa2b644d0ee0650e56c",  # Apr 24, 2016 (v1.0.0)
    importpath = "github.com/matttproud/golang_protobuf_extensions",
)

new_go_repository(
    name = "com_github_prometheus_procfs",
    commit = "1878d9fbb537119d24b21ca07effd591627cd160",  # Jan 28, 2017 (no releases)
    importpath = "github.com/prometheus/procfs",
)

new_go_repository(
    name = "com_github_beorn7_perks",
    commit = "4c0e84591b9aa9e6dcfdf3e020114cd81f89d5f9",  # Aug 4, 2016 (no releases)
    importpath = "github.com/beorn7/perks",
)

new_go_repository(
    name = "com_github_prometheus_client_model",
    commit = "fa8ad6fec33561be4280a8f0514318c79d7f6cb6",  # Feb 12, 2015 (only release too old)
    importpath = "github.com/prometheus/client_model",
)

new_go_repository(
    name = "com_github_cactus_statsd_client",
    commit = "91c326c3f7bd20f0226d3d1c289dd9f8ce28d33d",  # release 3.1.0, 5/30/2016
    importpath = "github.com/cactus/go-statsd-client",
)

new_go_repository(
    name = "com_github_redis_client",
    commit = "1ac54a28f5934ea5e08f588647e734aba2383cb8",  # Jan 28, 2017 (no releases)
    importpath = "github.com/mediocregopher/radix.v2",
)

new_go_repository(
    name = "com_github_mini_redis",
    commit = "e9169f14d501184b6cc94e270e5a93e4bab203d7",  # release 2.0.0, 4/15/2017
    importpath = "github.com/alicebob/miniredis",
)

new_go_repository(
    name = "com_github_bsm_redeo",
    commit = "1ce09fc76693fb3c1ca9b529c66f38920beb6fb8",  # Aug 17, 2016 (no releases)
    importpath = "github.com/bsm/redeo",
)

new_go_repository(
    name = "io_k8s_client_go",
    commit = "243d8a9cb66a51ad8676157f79e71033b4014a2a",  # Dec 11, 2016 (matches istio manager)
    importpath = "k8s.io/client-go",
)

new_go_repository(
    name = "com_github_ugorji_go",
    commit = "708a42d246822952f38190a8d8c4e6b16a0e600c",  # Mar 12, 2017 (no releases)
    importpath = "github.com/ugorji/go",
)

new_go_repository(
    name = "in_gopkg_inf_v0",
    commit = "3887ee99ecf07df5b447e9b00d9c0b2adaa9f3e4",  # Sep 11, 2015 (latest commit)
    importpath = "gopkg.in/inf.v0",
)

new_go_repository(
    name = "com_github_docker_distribution",
    commit = "a25b9ef0c9fe242ac04bb20d3a028442b7d266b6",  # Apr 5, 2017 (v2.6.1)
    importpath = "github.com/docker/distribution",
)

new_go_repository(
    name = "com_github_davecgh_go_spew",
    commit = "346938d642f2ec3594ed81d874461961cd0faa76",  # Oct 29, 2016 (v1.1.0)
    importpath = "github.com/davecgh/go-spew",
)

new_go_repository(
    name = "com_github_go_openapi_spec",
    commit = "6aced65f8501fe1217321abf0749d354824ba2ff",  # Aug 8, 2016 (no releases)
    importpath = "github.com/go-openapi/spec",
)

new_go_repository(
    name = "com_github_google_gofuzz",
    commit = "44d81051d367757e1c7c6a5a86423ece9afcf63c",  # Nov 22, 2016 (no releases)
    importpath = "github.com/google/gofuzz",
)

new_go_repository(
    name = "com_github_emicklei_go_restful",
    commit = "09691a3b6378b740595c1002f40c34dd5f218a22",  # Dec 12, 2016 (k8s deps)
    importpath = "github.com/emicklei/go-restful",
)

new_go_repository(
    name = "com_github_go_openapi_jsonpointer",
    commit = "46af16f9f7b149af66e5d1bd010e3574dc06de98",  # Jul 4, 2016 (no releases)
    importpath = "github.com/go-openapi/jsonpointer",
)

new_go_repository(
    name = "com_github_go_openapi_jsonreference",
    commit = "13c6e3589ad90f49bd3e3bbe2c2cb3d7a4142272",  # Jul 4, 2016 (no releases)
    importpath = "github.com/go-openapi/jsonreference",
)

new_go_repository(
    name = "com_github_go_openapi_swag",
    commit = "1d0bd113de87027671077d3c71eb3ac5d7dbba72",  # Jul 4, 2016 (no releases)
    importpath = "github.com/go-openapi/swag",
)

new_go_repository(
    name = "org_golang_x_oauth2",
    commit = "3c3a985cb79f52a3190fbc056984415ca6763d01",  # Aug 26, 2016 (no releases)
    importpath = "golang.org/x/oauth2",
)

new_go_repository(
    name = "com_github_juju_ratelimit",
    commit = "acf38b000a03e4ab89e40f20f1e548f4e6ac7f72",  # Mar 13, 2017 (no releases)
    importpath = "github.com/juju/ratelimit",
)

new_go_repository(
    name = "com_github_opencontainers_go_digest",
    commit = "aa2ec055abd10d26d539eb630a92241b781ce4bc",  # Jan 31, 2017 (v1.0.0-rc0)
    importpath = "github.com/opencontainers/go-digest",
)

new_go_repository(
    name = "com_github_blang_semver",
    commit = "b38d23b8782a487059e8fc8773e9a5b228a77cb6",  # Jan 30, 2017 (v3.5.0)
    importpath = "github.com/blang/semver",
)

new_go_repository(
    name = "com_github_coreos_go_oidc",
    commit = "be73733bb8cc830d0205609b95d125215f8e9c70",  # Mar 7, 2017 (no releases)
    importpath = "github.com/coreos/go-oidc",
)

new_go_repository(
    name = "com_github_mailru_easyjson",
    commit = "2af9a745a611440bab0528e5ac19b2805a1c50eb",  # Mar 28, 2017 (no releases)
    importpath = "github.com/mailru/easyjson",
)

new_go_repository(
    name = "com_github_PuerkitoBio_purell",
    commit = "0bcb03f4b4d0a9428594752bd2a3b9aa0a9d4bd4",  # Nov 14, 2016 (v1.1.0)
    importpath = "github.com/PuerkitoBio/purell",
)

new_go_repository(
    name = "org_golang_x_text",
    build_file_name = "BUILD.bazel",
    commit = "f4b4367115ec2de254587813edaa901bc1c723a8",  # Mar 31, 2017 (no releases)
    importpath = "golang.org/x/text",
)

new_go_repository(
    name = "com_github_PuerkitoBio_urlesc",
    commit = "bbf7a2afc14f93e1e0a5c06df524fbd75e5031e5",  # Mar 24, 2017 (no releases)
    importpath = "github.com/PuerkitoBio/urlesc",
)

new_go_repository(
    name = "com_github_pborman_uuid",
    commit = "a97ce2ca70fa5a848076093f05e639a89ca34d06",  # Feb 9, 2016 (v1.0)
    importpath = "github.com/pborman/uuid",
)

new_go_repository(
    name = "com_google_cloud_go",
    commit = "2e6a95edb1071d750f6d7db777bf66cd2997af6c",  # Mar 9, 2017 (v0.7.0)
    importpath = "cloud.google.com/go",
)

new_go_repository(
    name = "com_github_coreos_pkg",
    commit = "1c941d73110817a80b9fa6e14d5d2b00d977ce2a",  # Feb 6, 2017 (fix for build dir bazel issue)
    importpath = "github.com/coreos/pkg",
)

new_go_repository(
    name = "com_github_jonboulle_clockwork",
    commit = "2eee05ed794112d45db504eb05aa693efd2b8b09",  # Jul 6, 2016 (v0.1.0)
    importpath = "github.com/jonboulle/clockwork",
)

new_go_repository(
    name = "com_github_imdario_mergo",
    commit = "3e95a51e0639b4cf372f2ccf74c86749d747fbdc",  # Feb 16, 2016 (v0.2.2)
    importpath = "github.com/imdario/mergo",
)

new_go_repository(
    name = "com_github_howeyc_gopass",
    commit = "bf9dde6d0d2c004a008c27aaee91170c786f6db8",  # Jan 9, 2017 (no releases)
    importpath = "github.com/howeyc/gopass",
)

new_go_repository(
    name = "org_golang_x_crypto",
    commit = "cbc3d0884eac986df6e78a039b8792e869bff863",  # Apr 8, 2017 (no releases)
    importpath = "golang.org/x/crypto",
)

new_go_repository(
    name = "com_github_googleapis_gax_go",
    commit = "9af46dd5a1713e8b5cd71106287eba3cefdde50b", # Mar 20, 2017 (no releases)
    importpath = "github.com/googleapis/gax-go",
)

new_go_repository(
    name = "com_github_hashicorp_golang_lru",
    commit = "0a025b7e63adc15a622f29b0b2c4c3848243bbf6", # Aug 13, 2016 (no releases)
    importpath = "github.com/hashicorp/golang-lru",
)

new_go_repository(
    name = "com_github_grpcecosystem_opentracing",
    commit = "c94552f01d20ad74ec45a8cd967833a9d0b106cf", #
    importpath = "github.com/grpc-ecosystem/grpc-opentracing",
)

##
## Testing
##

git_repository(
    name = "istio_test_infra",
    commit = "983183f98b79f8b67fe380fef4cdd21481830fd7",  # Apr 13, 2017 (no releases)
    remote = "https://github.com/istio/test-infra.git",
)

############################################################

load("//src/envoy/mixer/integration_test:repositories.bzl", "go_mixer_repositories")
go_mixer_repositories()
