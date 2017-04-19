workspace(name = "com_github_istio_istio")

git_repository(
    name = "io_bazel_rules_go",
    commit = "87cdda3fc0fd65c63ef0316533be03ea4956f809",  # April 7 2017 (0.4.2)
    remote = "https://github.com/bazelbuild/rules_go.git",
)

load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "new_go_repository")

go_repositories()

new_go_repository(
    name = "com_github_golang_glog",
    commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",  # Jan 26, 2016 (no releases)
    importpath = "github.com/golang/glog",
)

new_go_repository(
    name = "com_google_cloud_go",
    commit = "57377bad3486b37af17b47230a61603794c798ae",
    importpath = "cloud.google.com/go",
)

new_go_repository(
    name = "org_golang_x_net",
    commit = "242b6b35177ec3909636b6cf6a47e8c2c6324b5d",
    importpath = "golang.org/x/net",
)

new_go_repository(
    name = "org_golang_x_oauth2",
    commit = "314dd2c0bf3ebd592ec0d20847d27e79d0dbe8dd",
    importpath = "golang.org/x/oauth2",
)

new_go_repository(
    name = "org_golang_google_api",
    commit = "48e49d1645e228d1c50c3d54fb476b2224477303",
    importpath = "google.golang.org/api",
)

new_go_repository(
    name = "org_golang_google_grpc",
    commit = "377586b314e142ce186a0644138c92fe55b9162e",
    importpath = "google.golang.org/grpc",
)

new_go_repository(
    name = "org_golang_google_genproto",
    commit = "411e09b969b1170a9f0c467558eb4c4c110d9c77",
    importpath = "google.golang.org/genproto",
)

new_go_repository(
    name = "com_github_googleapis_gax_go",
    commit = "9af46dd5a1713e8b5cd71106287eba3cefdde50b",
    importpath = "github.com/googleapis/gax-go",
)

new_go_repository(
    name = "com_github_google_uuid",
    commit = "6a5e28554805e78ea6141142aba763936c4761c0",
    importpath = "github.com/google/uuid",
)

new_go_repository(
    name = "com_github_golang_protobuf",
    commit = "2bba0603135d7d7f5cb73b2125beeda19c09f4ef",
    importpath = "github.com/golang/protobuf",
)
