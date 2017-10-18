workspace(name = "com_github_istio_pilot")

git_repository(
    name = "io_bazel_rules_go",
    commit = "0e5a0e51b4e9fc3b5ef1436639a43fce27559744",  # Oct 3, 2017
    remote = "https://github.com/bazelbuild/rules_go.git",
)

git_repository(
    name = "com_github_google_protobuf",
    commit = "593e917c176b5bc5aafa57bf9f6030d749d91cd5",  # Jan 2017 3.2.0
    remote = "https://github.com/google/protobuf.git",
)

load("@io_bazel_rules_go//go:def.bzl", "go_rules_dependencies", "go_repository", "go_register_toolchains")

go_rules_dependencies()
go_register_toolchains()

##
## Kubernetes dependencies
##

go_repository(
    name = "com_google_cloud_go",
    commit = "3b1ae45394a234c385be014e9a488f2bb6eef821",
    importpath = "cloud.google.com/go",
)

go_repository(
    name = "com_github_Azure_go_autorest",
    commit = "58f6f26e200fa5dfb40c9cd1c83f3e2c860d779d",
    importpath = "github.com/Azure/go-autorest",
)

go_repository(
    name = "com_github_PuerkitoBio_purell",
    commit = "8a290539e2e8629dbc4e6bad948158f790ec31f4",
    importpath = "github.com/PuerkitoBio/purell",
)

go_repository(
    name = "com_github_PuerkitoBio_urlesc",
    commit = "5bd2802263f21d8788851d5305584c82a5c75d7e",
    importpath = "github.com/PuerkitoBio/urlesc",
)

go_repository(
    name = "com_github_coreos_go_oidc",
    commit = "be73733bb8cc830d0205609b95d125215f8e9c70",
    importpath = "github.com/coreos/go-oidc",
)

go_repository(
    name = "com_github_coreos_pkg",
    commit = "fa29b1d70f0beaddd4c7021607cc3c3be8ce94b8",
    importpath = "github.com/coreos/pkg",
)

go_repository(
    name = "com_github_davecgh_go_spew",
    commit = "782f4967f2dc4564575ca782fe2d04090b5faca8",
    importpath = "github.com/davecgh/go-spew",
)

go_repository(
    name = "com_github_dgrijalva_jwt_go",
    commit = "01aeca54ebda6e0fbfafd0a524d234159c05ec20",
    importpath = "github.com/dgrijalva/jwt-go",
)

go_repository(
    name = "com_github_docker_spdystream",
    commit = "449fdfce4d962303d702fec724ef0ad181c92528",
    importpath = "github.com/docker/spdystream",
)

go_repository(
    name = "com_github_emicklei_go_restful",
    commit = "ff4f55a206334ef123e4f79bbf348980da81ca46",
    importpath = "github.com/emicklei/go-restful",
)

go_repository(
    name = "com_github_emicklei_go_restful_swagger12",
    commit = "dcef7f55730566d41eae5db10e7d6981829720f6",
    importpath = "github.com/emicklei/go-restful-swagger12",
)

go_repository(
    name = "com_github_ghodss_yaml",
    commit = "73d445a93680fa1a78ae23a5839bad48f32ba1ee",
    importpath = "github.com/ghodss/yaml",
)

go_repository(
    name = "com_github_go_openapi_jsonpointer",
    commit = "46af16f9f7b149af66e5d1bd010e3574dc06de98",
    importpath = "github.com/go-openapi/jsonpointer",
)

go_repository(
    name = "com_github_go_openapi_jsonreference",
    commit = "13c6e3589ad90f49bd3e3bbe2c2cb3d7a4142272",
    importpath = "github.com/go-openapi/jsonreference",
)

go_repository(
    name = "com_github_go_openapi_spec",
    commit = "6aced65f8501fe1217321abf0749d354824ba2ff",
    importpath = "github.com/go-openapi/spec",
)

go_repository(
    name = "com_github_go_openapi_swag",
    commit = "1d0bd113de87027671077d3c71eb3ac5d7dbba72",
    importpath = "github.com/go-openapi/swag",
)

go_repository(
    name = "com_github_gogo_protobuf",
    commit = "c0656edd0d9eab7c66d1eb0c568f9039345796f7",
    importpath = "github.com/gogo/protobuf",
)

go_repository(
    name = "com_github_golang_glog",
    commit = "44145f04b68cf362d9c4df2182967c2275eaefed",
    importpath = "github.com/golang/glog",
)

go_repository(
    name = "com_github_golang_groupcache",
    commit = "02826c3e79038b59d737d3b1c0a1d937f71a4433",
    importpath = "github.com/golang/groupcache",
)

go_repository(
    name = "com_github_google_gofuzz",
    commit = "44d81051d367757e1c7c6a5a86423ece9afcf63c",
    importpath = "github.com/google/gofuzz",
)

go_repository(
    name = "com_github_howeyc_fsnotify",
    commit = "f0c08ee9c60704c1879025f2ae0ff3e000082c13",
    importpath = "github.com/howeyc/fsnotify",
)

go_repository(
    name = "com_github_googleapis_gnostic",
    commit = "68f4ded48ba9414dab2ae69b3f0d69971da73aa5",
    importpath = "github.com/googleapis/gnostic",
)

go_repository(
    name = "com_github_hashicorp_golang_lru",
    commit = "a0d98a5f288019575c6d1f4bb1573fef2d1fcdc4",
    importpath = "github.com/hashicorp/golang-lru",
)

go_repository(
    name = "com_github_howeyc_gopass",
    commit = "bf9dde6d0d2c004a008c27aaee91170c786f6db8",
    importpath = "github.com/howeyc/gopass",
)

go_repository(
    name = "com_github_imdario_mergo",
    commit = "6633656539c1639d9d78127b7d47c622b5d7b6dc",
    importpath = "github.com/imdario/mergo",
)

go_repository(
    name = "com_github_jonboulle_clockwork",
    commit = "72f9bd7c4e0c2a40055ab3d0f09654f730cce982",
    importpath = "github.com/jonboulle/clockwork",
)

go_repository(
    name = "com_github_juju_ratelimit",
    commit = "5b9ff866471762aa2ab2dced63c9fb6f53921342",
    importpath = "github.com/juju/ratelimit",
)

go_repository(
    name = "com_github_mailru_easyjson",
    commit = "d5b7844b561a7bc640052f1b935f7b800330d7e0",
    importpath = "github.com/mailru/easyjson",
)

go_repository(
    name = "com_github_mitchellh_go_homedir",
    commit = "b8bc1bf767474819792c23f32d8286a45736f1c6",
    importpath = "github.com/mitchellh/go-homedir",
)

go_repository(
    name = "com_github_pmezard_go_difflib",
    commit = "d8ed2627bdf02c080bf22230dbb337003b7aba2d",
    importpath = "github.com/pmezard/go-difflib",
)

go_repository(
    name = "com_github_stretchr_testify",
    commit = "f6abca593680b2315d2075e0f5e2a9751e3f431a",
    importpath = "github.com/stretchr/testify",
)

go_repository(
    name = "com_github_ugorji_go",
    commit = "ded73eae5db7e7a0ef6f55aace87a2873c5d2b74",
    importpath = "github.com/ugorji/go",
)

go_repository(
    name = "org_golang_x_crypto",
    commit = "d172538b2cfce0c13cee31e647d0367aa8cd2486",
    importpath = "golang.org/x/crypto",
)

go_repository(
    name = "org_golang_x_net",
    commit = "f2499483f923065a842d38eb4c7f1927e6fc6e6d",
    importpath = "golang.org/x/net",
)

go_repository(
    name = "org_golang_x_oauth2",
    commit = "a6bd8cefa1811bd24b86f8902872e4e8225f74c4",
    importpath = "golang.org/x/oauth2",
)

go_repository(
    name = "org_golang_x_time",
    commit = "8be79e1e0910c292df4e79c241bb7e8f7e725959",
    importpath = "golang.org/x/time",
)

go_repository(
    name = "org_golang_x_sys",
    commit = "8f0908ab3b2457e2e15403d3697c9ef5cb4b57a9",
    importpath = "golang.org/x/sys",
)

go_repository(
    name = "org_golang_x_text",
    build_file_name = "BUILD.bazel",  # darwin build: case insensitive file system problem
    commit = "2910a502d2bf9e43193af9d68ca516529614eed3",
    importpath = "golang.org/x/text",
)

go_repository(
    name = "in_gopkg_inf_v0",
    commit = "3887ee99ecf07df5b447e9b00d9c0b2adaa9f3e4",
    importpath = "gopkg.in/inf.v0",
)

go_repository(
    name = "in_gopkg_yaml_v2",
    commit = "53feefa2559fb8dfa8d81baad31be332c97d6c77",
    importpath = "gopkg.in/yaml.v2",
)

go_repository(
    name = "io_k8s_api",
    build_file_generation = "on",
    build_file_name = "BUILD.bazel",
    commit = "4d5cc6efc5e84aa19fb1bd3f911c16a6723c1bb7",
    importpath = "k8s.io/api",
)

go_repository(
    name = "io_k8s_apimachinery",
    build_file_generation = "on",
    build_file_name = "BUILD.bazel",
    commit = "6134cb2da6d90597b0434e349f90f94fafc9ae51",
    importpath = "k8s.io/apimachinery",
)

go_repository(
    name = "io_k8s_apiserver",
    build_file_generation = "on",
    build_file_name = "BUILD.bazel",
    commit = "149fc2228647cea28b0670c240ec582e985e8eda",  # Jul Aug 1, 2017
    importpath = "k8s.io/apiserver",
)

go_repository(
    name = "io_k8s_client_go",
    build_file_generation = "on",
    build_file_name = "BUILD.bazel",
    commit = "7c69e980210777a6292351ac6873de083526f08e",  # Jul 18, 2017
    importpath = "k8s.io/client-go",
)

# End of k8s dependencies

go_repository(
    name = "io_k8s_apiextensions_apiserver",
    build_file_generation = "on",
    build_file_name = "BUILD.bazel",
    commit = "c682349b0d1c12975d8e24a9799b66747255d7a5",
    importpath = "k8s.io/apiextensions-apiserver",
)

go_repository(
    name = "com_github_pkg_errors",
    commit = "a22138067af1c4942683050411a841ade67fe1eb",
    importpath = "github.com/pkg/errors",
)

go_repository(
    name = "io_k8s_ingress",
    commit = "0c6f15e372c831de52fcc393932540bb3a6d51b5",
    importpath = "k8s.io/ingress",
)

##
## Go dependencies
##

go_repository(
    name = "com_github_satori_go_uuid",
    commit = "5bf94b69c6b68ee1b541973bb8e1144db23a194b",
    importpath = "github.com/satori/go.uuid",
)

go_repository(
    name = "com_github_hashicorp_errwrap",
    commit = "7554cd9344cec97297fa6649b055a8c98c2a1e55",
    importpath = "github.com/hashicorp/errwrap",
)

go_repository(
    name = "com_github_hashicorp_go_multierror",
    commit = "8484912a3b9987857bac52e0c5fec2b95f419628",
    importpath = "github.com/hashicorp/go-multierror",
)

go_repository(
    name = "com_github_gorilla_websocket",
    commit = "a69d9f6de432e2c6b296a947d8a5ee88f68522cf",
    importpath = "github.com/gorilla/websocket",
)

# likely you need to update those next 2 at the same time:

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

go_repository(
    name = "com_github_inconshreveable_mousetrap",
    commit = "76626ae9c91c4f2a10f34cad8ce83ea42c93bb75",
    importpath = "github.com/inconshreveable/mousetrap",
)

go_repository(
    name = "com_github_cpuguy83_go_md2man",
    commit = "648eed146d3f3beacb64063cd0daae908015eebd",  # Mar 19, 2017 (no releases)
    importpath = "github.com/cpuguy83/go-md2man",
)

go_repository(
    name = "com_github_russross_blackfriday",
    commit = "4048872b16cc0fc2c5fd9eacf0ed2c2fedaa0c8c",
    importpath = "github.com/russross/blackfriday",
)

go_repository(
    name = "com_github_shurcooL_sanitized_anchor_name",
    commit = "10ef21a441db47d8b13ebcc5fd2310f636973c77",  # Mar 19, 2017 (no releases)
    importpath = "github.com/shurcooL/sanitized_anchor_name",
)

go_repository(
    name = "com_github_golang_sync",
    commit = "450f422ab23cf9881c94e2db30cac0eb1b7cf80c",
    importpath = "github.com/golang/sync",
)

go_repository(
    name = "org_golang_google_api",
    commit = "48e49d1645e228d1c50c3d54fb476b2224477303",
    importpath = "google.golang.org/api",
)

go_repository(
    name = "com_github_googleapis_gax_go",
    commit = "9af46dd5a1713e8b5cd71106287eba3cefdde50b",
    importpath = "github.com/googleapis/gax-go",
)

go_repository(
    name = "org_golang_google_genproto",
    commit = "411e09b969b1170a9f0c467558eb4c4c110d9c77",
    importpath = "google.golang.org/genproto",
)

go_repository(
    name = "com_github_hashicorp_go_rootcerts",
    commit = "6bb64b370b90e7ef1fa532be9e591a81c3493e00",  # May 3 2016
    importpath = "github.com/hashicorp/go-rootcerts",
)

go_repository(
    name = "com_github_hashicorp_go_cleanhttp",
    commit = "3573b8b52aa7b37b9358d966a898feb387f62437",  # Feb 10 2017
    importpath = "github.com/hashicorp/go-cleanhttp",
)

go_repository(
    name = "com_github_hashicorp_serf",
    commit = "d6574a5bb1226678d7010325fb6c985db20ee458",  # Feb 6 2017 v0.8.1
    importpath = "github.com/hashicorp/serf",
)

go_repository(
    name = "com_github_hashicorp_consul",
    commit = "f4360770d8e7b852e2d05835b583d20799e58133",  # Jun 9 2017 v0.8.4
    importpath = "github.com/hashicorp/consul",
)

##
## Proxy image
##

# Change this and the docker/Dockerfile.proxy* files together
# This SHA is obtained from proxy/postsubmit job
ISTIO_PROXY_BUCKET = "1509b9361c3e727b40146dcddd44ed0012b7de78"

http_file(
    name = "envoy_binary",
    url = "https://storage.googleapis.com/istio-build/proxy/envoy-debug-" + ISTIO_PROXY_BUCKET + ".tar.gz",
)

##
## Protobuf codegen rules
##

# Note: do not use go_proto_repositories since it has old versions of protobuf and grpc-go

go_repository(
    name = "com_github_golang_protobuf",
    commit = "8ee79997227bf9b34611aee7946ae64735e6fd93",
    importpath = "github.com/golang/protobuf",
)

go_repository(
    name = "org_golang_google_grpc",
    commit = "8050b9cbc271307e5a716a9d782803d09b0d6f2d",  # v1.2.1
    importpath = "google.golang.org/grpc",
)

# This SHA is obtained from istio/api
ISTIO_API = "93848bb1c495ff6ebc1a938e2e0a403d52d345c1"

new_git_repository(
    name = "io_istio_api",
    build_file_content = """
load("@io_bazel_rules_go//go:def.bzl", "go_prefix")
load("@io_bazel_rules_go//proto:go_proto_library.bzl", "go_proto_library")
package(default_visibility = ["//visibility:public"])
go_prefix("istio.io/api/proxy/v1/config")
go_proto_library(
    name = "go_default_library",
    srcs = glob(["proxy/v1/config/*.proto"]),
    deps = [
        "@com_github_golang_protobuf//ptypes/any:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
    ],
)
filegroup(
    name = "mixer",
    srcs = glob(["mixer/v1/*.proto"]),
)
filegroup(
    name = "wordlist",
    srcs = ["mixer/v1/global_dictionary.yaml"],
)
    """,
    commit = ISTIO_API,
    remote = "https://github.com/istio/api.git",
)

GOOGLEAPIS_BUILD_FILE = """
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix")
load("@io_bazel_rules_go//proto:go_proto_library.bzl", "go_proto_library")
go_prefix("github.com/googleapis/googleapis/google/rpc")

go_proto_library(
    name = "go_default_library",
    srcs = [
          "google/rpc/code.proto",
          "google/rpc/error_details.proto",
          "google/rpc/status.proto",
    ],
    deps = [
          "@com_github_golang_protobuf//ptypes/any:go_default_library",
          "@com_github_golang_protobuf//ptypes/duration:go_default_library",
          "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
    ],
)
"""

new_git_repository(
    name = "com_github_googleapis_googleapis",
    build_file_content = GOOGLEAPIS_BUILD_FILE,
    commit = "13ac2436c5e3d568bd0e938f6ed58b77a48aba15",  # Oct 21, 2016 (only release pre-dates sha)
    remote = "https://github.com/googleapis/googleapis.git",
)

##
## Mock codegen rules
##

go_repository(
    name = "com_github_golang_mock",
    commit = "bd3c8e81be01eef76d4b503f5e687d2d1354d2d9",
    importpath = "github.com/golang/mock",
)

##
## Testing
##

git_repository(
    name = "com_github_istio_test_infra",
    commit = "67e73ad01f9d1074a7d787a91201d41938ad4310",  # Aug 25, 2017
    remote = "https://github.com/istio/test-infra.git",
)
