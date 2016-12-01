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
#include "src/api_manager/service_control/url.h"

#include "gtest/gtest.h"
#include "src/api_manager/config.h"
#include "src/api_manager/mock_api_manager_environment.h"

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
