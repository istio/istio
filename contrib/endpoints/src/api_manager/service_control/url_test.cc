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
#include "contrib/endpoints/src/api_manager/service_control/url.h"

#include "contrib/endpoints/src/api_manager/config.h"
#include "contrib/endpoints/src/api_manager/mock_api_manager_environment.h"
#include "gtest/gtest.h"

using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace service_control {

namespace {

const char prepend_https_config[] = R"(
name: "https-config"
control: {
    environment: "servicecontrol.googleapis.com"
}
)";

static char server_config[] = R"(
service_control_config {
  url_override: "servicecontrol-testing.googleapis.com"
}
)";

TEST(UrlTest, PrependHttps) {
  std::unique_ptr<ApiManagerEnvInterface> env(
      new ::testing::NiceMock<MockApiManagerEnvironmentWithLog>());
  std::unique_ptr<Config> config(
      Config::Create(env.get(), prepend_https_config, ""));
  ASSERT_TRUE(config);
  Url url(&config->service(), config->server_config());
  // https:// got prepended by default.
  ASSERT_EQ("https://servicecontrol.googleapis.com", url.service_control());
  ASSERT_EQ(
      "https://servicecontrol.googleapis.com/v1/services/https-config:check",
      url.check_url());
  ASSERT_EQ(
      "https://servicecontrol.googleapis.com/v1/services/https-config:report",
      url.report_url());
  ASSERT_EQ(
      "https://servicecontrol.googleapis.com/v1/services/"
      "https-config:allocateQuota",
      url.quota_url());
}

TEST(UrlTest, ServerControlOverride) {
  std::unique_ptr<ApiManagerEnvInterface> env(
      new ::testing::NiceMock<MockApiManagerEnvironmentWithLog>());
  std::unique_ptr<Config> config(
      Config::Create(env.get(), prepend_https_config, server_config));
  ASSERT_TRUE(config);
  ASSERT_TRUE(config->server_config());
  Url url(&config->service(), config->server_config());
  ASSERT_EQ("https://servicecontrol-testing.googleapis.com",
            url.service_control());
}

}  // namespace

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
