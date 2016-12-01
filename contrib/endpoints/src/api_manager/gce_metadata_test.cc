// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
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
