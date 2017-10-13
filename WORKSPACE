# Use go get github.com/bazelbuild/rules_go/go/tools/wtool
# then for instance wtool -verbose com_github_golang_glog
# to add to this file

workspace(name = "com_github_istio_istio")

git_repository(
    name = "io_bazel_rules_go",
    commit = "9cf23e2aab101f86e4f51d8c5e0f14c012c2161c",  # Oct 12, 2017 (Add `build_external` option to `go_repository`)
    remote = "https://github.com/bazelbuild/rules_go.git",
)

load("@io_bazel_rules_go//go:def.bzl", "go_rules_dependencies", "go_register_toolchains")
go_rules_dependencies()
go_register_toolchains(go_version="1.8.3")

load("@io_bazel_rules_go//proto:def.bzl", "proto_register_toolchains")
proto_register_toolchains()

git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "ff3b7e7963daa7cb3b42f8936bc11eda4b960926",  # Oct 03, 2017 (Updating External Import Paths)
    remote = "https://github.com/pubref/rules_protobuf",
)

load("@io_bazel_rules_go//go:def.bzl", "go_repository")

bind(
    name = "protoc",
    actual = "@com_google_protobuf//:protoc",
)

bind(
    name = "protocol_compiler",
    actual = "@com_google_protobuf//:protoc",
)

go_repository(
    name = "org_golang_x_net",
    commit = "f5079bd7f6f74e23c4d65efa0f4ce14cbd6a3c0f",  # Jul 26, 2017 (no releases)
    importpath = "golang.org/x/net",
)

go_repository(
    name = "com_github_golang_glog",
    commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",  # Jan 26, 2016 (no releases)
    importpath = "github.com/golang/glog",
)

go_repository(
    name = "com_github_golang_protobuf",
    commit = "17ce1425424ab154092bbb43af630bd647f3bb0d",  # Nov 16, 2016 (match pubref dep)
    importpath = "github.com/golang/protobuf",
)

go_repository(
    name = "com_github_gogo_protobuf",
    commit = "100ba4e885062801d56799d78530b73b178a78f3",  # Mar 7, 2017 (match pubref dep)
    importpath = "github.com/gogo/protobuf",
    build_file_proto_mode = "legacy",
)

go_repository(
    name = "org_golang_google_grpc",
    commit = "f92cdcd7dcdc69e81b2d7b338479a19a8723cfa3",  # Aug 30, 2017 (v1.6.0)
    importpath = "google.golang.org/grpc",
)

go_repository(
    name = "org_golang_x_text",
    build_file_name = "BUILD.bazel",
    commit = "f4b4367115ec2de254587813edaa901bc1c723a8",  # Mar 31, 2017 (no releases)
    importpath = "golang.org/x/text",
)

go_repository(
    name = "io_istio_fortio",
    commit = "v0.2.8",
    importpath = "istio.io/fortio",
)

go_repository(
    name = "com_github_golang_glog",
    commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",  # Jan 26, 2016 (no releases)
    importpath = "github.com/golang/glog",
)

go_repository(
    name = "com_google_cloud_go",
    commit = "57377bad3486b37af17b47230a61603794c798ae",
    importpath = "cloud.google.com/go",
)

go_repository(
    name = "org_golang_x_net",
    commit = "242b6b35177ec3909636b6cf6a47e8c2c6324b5d",
    importpath = "golang.org/x/net",
)

go_repository(
    name = "org_golang_x_oauth2",
    commit = "314dd2c0bf3ebd592ec0d20847d27e79d0dbe8dd",
    importpath = "golang.org/x/oauth2",
)

go_repository(
    name = "org_golang_x_sync",
    commit = "f52d1811a62927559de87708c8913c1650ce4f26",
    importpath = "golang.org/x/sync",
)

go_repository(
    name = "org_golang_google_api",
    commit = "48e49d1645e228d1c50c3d54fb476b2224477303",
    importpath = "google.golang.org/api",
)

go_repository(
    name = "org_golang_google_grpc",
    commit = "377586b314e142ce186a0644138c92fe55b9162e",
    importpath = "google.golang.org/grpc",
)

go_repository(
    name = "org_golang_google_genproto",
    commit = "411e09b969b1170a9f0c467558eb4c4c110d9c77",
    importpath = "google.golang.org/genproto",
)

go_repository(
    name = "com_github_googleapis_gax_go",
    commit = "9af46dd5a1713e8b5cd71106287eba3cefdde50b",
    importpath = "github.com/googleapis/gax-go",
)

go_repository(
    name = "com_github_google_uuid",
    commit = "6a5e28554805e78ea6141142aba763936c4761c0",
    importpath = "github.com/google/uuid",
)

go_repository(
    name = "com_github_golang_protobuf",
    commit = "2bba0603135d7d7f5cb73b2125beeda19c09f4ef",
    importpath = "github.com/golang/protobuf",
)

go_repository(
    name = "com_github_pmezard_go_difflib",
    commit = "d8ed2627bdf02c080bf22230dbb337003b7aba2d",
    importpath = "github.com/pmezard/go-difflib",
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

##
## Docker rules
##

new_http_archive(
    name = "docker_ubuntu",
    build_file_content = """
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")
docker_build(
  name = "xenial",
  tars = ["xenial/ubuntu-xenial-core-cloudimg-amd64-root.tar.gz"],
  visibility = ["//visibility:public"],
)
""",
    sha256 = "de31e6fcb843068965de5945c11a6f86399be5e4208c7299fb7311634fb41943",
    strip_prefix = "docker-brew-ubuntu-core-e406914e5f648003dfe8329b512c30c9ad0d2f9c",
    type = "zip",
    url = "https://codeload.github.com/tianon/docker-brew-ubuntu-core/zip/e406914e5f648003dfe8329b512c30c9ad0d2f9c",
)

http_file(
    name = "deb_iptables",
    sha256 = "d2cafb4f1860435ce69a4971e3af5f4bb20753054020f32e1b767e4ba79c0831",
    url = "http://mirrors.kernel.org/ubuntu/pool/main/i/iptables/iptables_1.6.0-2ubuntu3_amd64.deb",
)

http_file(
    name = "deb_libnfnetlink",
    sha256 = "fbaf9b8914a607e2a07e5525c6c9c0ecb71d70236f54ad185f4cc81b4541f6ba",
    url = "http://mirrors.kernel.org/ubuntu/pool/main/libn/libnfnetlink/libnfnetlink0_1.0.1-3_amd64.deb",
)

http_file(
    name = "deb_libxtables",
    sha256 = "9a4140b0b599612af1006efeee1c6b98771b0bc8dcdcd0510218ef69d6652c7f",
    url = "http://mirrors.kernel.org/ubuntu/pool/main/i/iptables/libxtables11_1.6.0-2ubuntu3_amd64.deb",
)

# More go repositories (wtool adds things at the end)

go_repository(
    name = "com_github_prometheus_common",
    commit = "13ba4ddd0caa9c28ca7b7bffe1dfa9ed8d5ef207",
    importpath = "github.com/prometheus/common",
)

go_repository(
    name = "com_github_prometheus_client_model",
    commit = "fa8ad6fec33561be4280a8f0514318c79d7f6cb6",  # Feb 12, 2015 (only release too old)
    importpath = "github.com/prometheus/client_model",
)

go_repository(
    name = "com_github_matttproud_golang_protobuf_extensions",
    commit = "c12348ce28de40eed0136aa2b644d0ee0650e56c",  # Apr 24, 2016 (v1.0.0)
    importpath = "github.com/matttproud/golang_protobuf_extensions",
)

go_repository(
    name = "com_github_prometheus_client_golang",
    commit = "de4d4ffe63b9eff7f27484fdef6e421597e6abb4",  # June 6, 2017
    importpath = "github.com/prometheus/client_golang",
)
