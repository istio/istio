// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/gce_metadata.h"
#include "gtest/gtest.h"

using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {

namespace {

const char metadata[] = R"(
{
  "instance": {
    "attributes": {
      "gae_server_software": "Google App Engine/1.9.38",
      "kube-env": "Kubernetes environment"
    },
    "zone": "projects/23479234856/zones/us-central1-f"
  },
  "project": {
    "numericProjectId": 23479234856,
    "projectId": "esp-test-app"
  }
})";

const char partial_metadata[] = R"(
{
  "instance": {
    "zone": "projects/23479234856/zones/us-central1-f"
  }
})";

TEST(Metadata, ExtractPropertyValue) {
  GceMetadata env;
  std::string meta_str(metadata);
  Status status = env.ParseFromJson(&meta_str);
  ASSERT_TRUE(status.ok());

  ASSERT_EQ("us-central1-f", env.zone());
  ASSERT_EQ("Google App Engine/1.9.38", env.gae_server_software());
  ASSERT_EQ("Kubernetes environment", env.kube_env());
  ASSERT_EQ("esp-test-app", env.project_id());
}

TEST(Metadata, ExtractSomePropertyValues) {
  GceMetadata env;
  std::string meta_str(partial_metadata);
  Status status = env.ParseFromJson(&meta_str);
  ASSERT_TRUE(status.ok());
  ASSERT_EQ("us-central1-f", env.zone());
  ASSERT_TRUE(env.gae_server_software().empty());
  ASSERT_TRUE(env.kube_env().empty());
}

TEST(Metadata, ExtractErrors) {
  std::string meta_str(metadata);
  std::string half_str = meta_str.substr(0, meta_str.size() / 2);
  // Half a Json to make it an invalid Json.
  GceMetadata env;
  Status status = env.ParseFromJson(&half_str);
  ASSERT_FALSE(status.ok());
}

}  // namespace

}  // namespace api_manager
}  // namespace google
