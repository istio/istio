git_repository(
    name = "io_bazel_rules_go",
    remote = "https://github.com/bazelbuild/rules_go.git",
    tag = "0.5.2",
)

load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "go_repository")

go_repositories()

git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "9ede1dbc38f0b89ae6cd8e206a22dd93cc1d5637",  # Mar 31, 2017 (gogo* support)
    remote = "https://github.com/pubref/rules_protobuf",
)

load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_repositories")

proto_repositories()

go_repository(
    name = "com_google_cloud_go",
    commit = "2e6a95edb1071d750f6d7db777bf66cd2997af6c",  # Mar 9, 2017 (v0.7.0)
    importpath = "cloud.google.com/go",
)

go_repository(
    name = "com_github_coreos_go_oidc",
    commit = "f828b1fc9b58b59bd70ace766bfc190216b58b01",
    importpath = "github.com/coreos/go-oidc",
)

go_repository(
    name = "com_github_coreos_pkg",
    commit = "1c941d73110817a80b9fa6e14d5d2b00d977ce2a",
    importpath = "github.com/coreos/pkg",
)

go_repository(
    name = "com_github_davecgh_go_spew",
    commit = "346938d642f2ec3594ed81d874461961cd0faa76",
    importpath = "github.com/davecgh/go-spew",
)

go_repository(
    name = "com_github_docker_distribution",
    commit = "62d8d910b5a00cfdc425d8d62faa1e86f69e3527",
    importpath = "github.com/docker/distribution",
)

go_repository(
    name = "com_github_emicklei_go_restful",
    commit = "ad3e7d5a0a11fbbead57cc9353720a60a0a2793f",
    importpath = "github.com/emicklei/go-restful",
)

go_repository(
    name = "com_github_ghodss_yaml",
    commit = "04f313413ffd65ce25f2541bfd2b2ceec5c0908c",
    importpath = "github.com/ghodss/yaml",
)

go_repository(
    name = "com_github_gogo_protobuf",
    commit = "2221ff550f109ae54cb617c0dc6ac62658c418d7",
    importpath = "github.com/gogo/protobuf",
)

go_repository(
    name = "com_github_googleapis_gax_go",
    commit = "da06d194a00e19ce00d9011a13931c3f6f6887c7",
    importpath = "github.com/googleapis/gax-go",
)

go_repository(
    name = "com_github_golang_glog",
    commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",
    importpath = "github.com/golang/glog",
)

go_repository(
    name = "com_github_golang_protobuf",
    commit = "69b215d01a5606c843240eab4937eab3acee6530",
    importpath = "github.com/golang/protobuf",
)

go_repository(
    name = "com_github_google_gofuzz",
    commit = "44d81051d367757e1c7c6a5a86423ece9afcf63c",
    importpath = "github.com/google/gofuzz",
)

go_repository(
    name = "com_github_go_openapi_jsonpointer",
    commit = "779f45308c19820f1a69e9a4cd965f496e0da10f",
    importpath = "github.com/go-openapi/jsonpointer",
)

go_repository(
    name = "com_github_go_openapi_jsonreference",
    commit = "36d33bfe519efae5632669801b180bf1a245da3b",
    importpath = "github.com/go-openapi/jsonreference",
)

go_repository(
    name = "com_github_go_openapi_spec",
    commit = "02fb9cd3430ed0581e0ceb4804d5d4b3cc702694",
    importpath = "github.com/go-openapi/spec",
)

go_repository(
    name = "com_github_go_openapi_swag",
    commit = "d5f8ebc3b1c55a4cf6489eeae7354f338cfe299e",
    importpath = "github.com/go-openapi/swag",
)

go_repository(
    name = "com_github_howeyc_gopass",
    commit = "bf9dde6d0d2c004a008c27aaee91170c786f6db8",
    importpath = "github.com/howeyc/gopass",
)

go_repository(
    name = "com_github_imdario_mergo",
    commit = "50d4dbd4eb0e84778abe37cefef140271d96fade",
    importpath = "github.com/imdario/mergo",
)

go_repository(
    name = "com_github_jonboulle_clockwork",
    commit = "bcac9884e7502bb2b474c0339d889cb981a2f27f",
    importpath = "github.com/jonboulle/clockwork",
)

go_repository(
    name = "com_github_juju_ratelimit",
    commit = "77ed1c8a01217656d2080ad51981f6e99adaa177",
    importpath = "github.com/juju/ratelimit",
)

go_repository(
    name = "com_github_mailru_easyjson",
    commit = "99e922cf9de1bc0ab38310c277cff32c2147e747",
    importpath = "github.com/mailru/easyjson",
)

go_repository(
    name = "com_github_opencontainers_go_digest",
    commit = "aa2ec055abd10d26d539eb630a92241b781ce4bc",
    importpath = "github.com/opencontainers/go-digest",
)

go_repository(
    name = "com_github_PuerkitoBio_purell",
    commit = "0bcb03f4b4d0a9428594752bd2a3b9aa0a9d4bd4",
    importpath = "github.com/PuerkitoBio/purell",
)

go_repository(
    name = "com_github_PuerkitoBio_urlesc",
    commit = "5bd2802263f21d8788851d5305584c82a5c75d7e",
    importpath = "github.com/PuerkitoBio/urlesc",
)

go_repository(
    name = "com_github_pborman_uuid",
    commit = "1b00554d822231195d1babd97ff4a781231955c9",
    importpath = "github.com/pborman/uuid",
)

go_repository(
    name = "com_github_spf13_cobra",
    commit = "4cdb38c072b86bf795d2c81de50784d9fdd6eb77",
    importpath = "github.com/spf13/cobra",
)

go_repository(
    name = "com_github_spf13_pflag",
    commit = "e57e3eeb33f795204c1ca35f56c44f83227c6e66",
    importpath = "github.com/spf13/pflag",
)

go_repository(
    name = "com_github_ugorji_go",
    commit = "d23841a297e5489e787e72fceffabf9d2994b52a",
    importpath = "github.com/ugorji/go",
)

go_repository(
    name = "com_google_cloud_go",
    commit = "1ed2f0abb2869a51b3a5b9daec801bf9791f95d0",
    importpath = "cloud.google.com/go",
)

go_repository(
    name = "in_gopkg_inf_v0",
    commit = "3887ee99ecf07df5b447e9b00d9c0b2adaa9f3e4",
    importpath = "gopkg.in/inf.v0",
)

go_repository(
    name = "in_gopkg_yaml_v2",
    commit = "a3f3340b5840cee44f372bddb5880fcbc419b46a",
    importpath = "gopkg.in/yaml.v2",
)

go_repository(
    name = "io_k8s_apimachinery",
    commit = "d3c1641d0c440b4c1bef7e1fc105f19f713477e0",
    importpath = "k8s.io/apimachinery",
)

go_repository(
    name = "io_k8s_client_go",
    commit = "86a2be1b447d7abddae88de0a5f642935992b803",
    importpath = "k8s.io/client-go",
)

go_repository(
    name = "org_golang_google_grpc",
    commit = "20633fa172ac711ac6a77fd573ed13f23ec56dcb",
    importpath = "google.golang.org/grpc",
)

go_repository(
    name = "org_golang_x_crypto",
    commit = "728b753d0135da6801d45a38e6f43ff55779c5c2",
    importpath = "golang.org/x/crypto",
)

go_repository(
    name = "org_golang_x_net",
    commit = "61557ac0112b576429a0df080e1c2cef5dfbb642",
    importpath = "golang.org/x/net",
)

go_repository(
    name = "org_golang_x_oauth2",
    commit = "b9780ec78894ab900c062d58ee3076cd9b2a4501",
    importpath = "golang.org/x/oauth2",
)

go_repository(
    name = "org_golang_x_text",
    commit = "06d6eba81293389cafdff7fca90d75592194b2d9",
    importpath = "golang.org/x/text",
)

go_repository(
    name = "org_golang_google_api",
    commit = "48e49d1645e228d1c50c3d54fb476b2224477303",
    importpath = "google.golang.org/api",
)

go_repository(
    name = "org_golang_google_genproto",
    commit = "411e09b969b1170a9f0c467558eb4c4c110d9c77",
    importpath = "google.golang.org/genproto",
)

new_http_archive(
    name = "docker_ubuntu",
    build_file = "BUILD.ubuntu",
    sha256 = "2c63dd81d714b825acd1cb3629c57d6ee733645479d0fcdf645203c2c35924c5",
    type = "zip",
    url = "https://codeload.github.com/tianon/docker-brew-ubuntu-core/zip/b6f1fe19228e5b6b7aed98dcba02f18088282f90",
)



GOOGLEAPIS_BUILD_FILE = """
package(default_visibility = ["//visibility:public"])
load("@io_bazel_rules_go//go:def.bzl", "go_prefix")
go_prefix("github.com/googleapis/googleapis")
load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")
gogoslick_proto_library(
    name = "google/rpc",
    protos = [
        "google/rpc/code.proto",
        "google/rpc/error_details.proto",
        "google/rpc/status.proto",
    ],
    importmap = {
        "google/protobuf/any.proto": "github.com/gogo/protobuf/types",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
    verbose = 0,
)
load("@org_pubref_rules_protobuf//cpp:rules.bzl", "cc_proto_library")
cc_proto_library(
    name = "cc_status_proto",
    protos = [
        "google/rpc/status.proto",
    ],
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    verbose = 0,
)
filegroup(
    name = "status_proto",
    srcs = [ "google/rpc/status.proto" ],
)
filegroup(
    name = "code_proto",
    srcs = [ "google/rpc/code.proto" ],
)
"""

new_git_repository(
    name = "com_github_googleapis_googleapis",
    build_file_content = GOOGLEAPIS_BUILD_FILE,
    commit = "13ac2436c5e3d568bd0e938f6ed58b77a48aba15",  # Oct 21, 2016 (only release pre-dates sha)
    remote = "https://github.com/googleapis/googleapis.git",
)

##
## Repo containing utilities for testing
##

git_repository(
    name = "com_github_istio_test_infra",
    commit = "b0822890273f91d5aa8c40ea1a89ba01e0f0ee9d",   # Aug 22, 2017
    remote = "https://github.com/istio/test-infra.git",
)

