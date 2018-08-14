/* Copyright 2017 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "proxy/src/envoy/utils/utils.h"
#include "mixer/v1/config/client/client_config.pb.h"
#include "test/test_common/utility.h"

using Envoy::Utils::ParseJsonMessage;

namespace {

TEST(UtilsTest, ParseNormalMessage) {
  std::string config_str = R"({
        "default_destination_service": "service.svc.cluster.local",
      })";
  ::istio::mixer::v1::config::client::HttpClientConfig http_config;

  auto status = ParseJsonMessage(config_str, &http_config);
  EXPECT_OK(status) << status;
  EXPECT_EQ(http_config.default_destination_service(),
            "service.svc.cluster.local");
}

TEST(UtilsTest, ParseMessageWithUnknownField) {
  std::string config_str = R"({
        "default_destination_service": "service.svc.cluster.local",
        "unknown_field": "xxx",
      })";
  ::istio::mixer::v1::config::client::HttpClientConfig http_config;

  EXPECT_OK(ParseJsonMessage(config_str, &http_config));
  EXPECT_EQ(http_config.default_destination_service(),
            "service.svc.cluster.local");
}
}  // namespace
