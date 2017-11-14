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

#include "client_context.h"
#include "control/src/mock_mixer_client.h"
#include "controller_impl.h"
#include "gtest/gtest.h"
#include "mock_check_data.h"
#include "mock_report_data.h"

using ::google::protobuf::util::Status;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::config::client::ServiceConfig;
using ::istio::mixer::v1::config::client::HttpClientConfig;
using ::istio::mixer_client::CancelFunc;
using ::istio::mixer_client::TransportCheckFunc;
using ::istio::mixer_client::DoneFunc;
using ::istio::mixer_client::MixerClient;

using ::testing::_;
using ::testing::Invoke;

namespace istio {
namespace mixer_control {
namespace http {

class RequestHandlerImplTest : public ::testing::Test {
 public:
  void SetUp() {
    mock_client_ = new ::testing::NiceMock<MockMixerClient>;
    client_context_ = std::make_shared<ClientContext>(
        std::unique_ptr<MixerClient>(mock_client_), client_config_);
    controller_ =
        std::unique_ptr<Controller>(new ControllerImpl(client_context_));
  }

  std::shared_ptr<ClientContext> client_context_;
  HttpClientConfig client_config_;
  ::testing::NiceMock<MockMixerClient>* mock_client_;
  std::unique_ptr<Controller> controller_;
};

TEST_F(RequestHandlerImplTest, TestHandlerDisabledCheckReport) {
  ::testing::NiceMock<MockCheckData> mock_data;
  // Not to extract attributes since both Check and Report are disabled.
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(0);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(0);

  // Check should NOT be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _)).Times(0);

  ServiceConfig legacy;
  legacy.set_enable_mixer_check(false);
  legacy.set_enable_mixer_report(false);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, nullptr,
                 [](const Status& status) { EXPECT_TRUE(status.ok()); });
}

TEST_F(RequestHandlerImplTest, TestHandlerDisabledCheck) {
  ::testing::NiceMock<MockCheckData> mock_data;
  // Report is enabled so Attributes are extracted.
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should NOT be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _)).Times(0);

  ServiceConfig legacy;
  legacy.set_enable_mixer_check(false);
  legacy.set_enable_mixer_report(true);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, nullptr,
                 [](const Status& status) { EXPECT_TRUE(status.ok()); });
}

TEST_F(RequestHandlerImplTest, TestHandlerMixerAttributes) {
  ::testing::NiceMock<MockCheckData> mock_data;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _))
      .WillOnce(
          Invoke([](const Attributes& attributes, TransportCheckFunc transport,
                    DoneFunc on_done) -> CancelFunc {
            auto map = attributes.attributes();
            EXPECT_EQ(map["key1"].string_value(), "value1");
            EXPECT_EQ(map["key2"].string_value(), "value2");
            return nullptr;
          }));

  ServiceConfig legacy;
  legacy.set_enable_mixer_check(true);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto map1 = client_config_.mutable_mixer_attributes()->mutable_attributes();
  (*map1)["key1"].set_string_value("value1");
  auto map2 = legacy.mutable_mixer_attributes()->mutable_attributes();
  (*map2)["key2"].set_string_value("value2");

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestHandlerCheck) {
  ::testing::NiceMock<MockCheckData> mock_data;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _)).Times(1);

  ServiceConfig legacy;
  legacy.set_enable_mixer_check(true);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestHandlerReport) {
  ::testing::NiceMock<MockReportData> mock_data;
  EXPECT_CALL(mock_data, GetResponseHeaders()).Times(1);
  EXPECT_CALL(mock_data, GetReportInfo(_)).Times(1);

  // Report should be called.
  EXPECT_CALL(*mock_client_, Report(_)).Times(1);

  ServiceConfig legacy;
  legacy.set_enable_mixer_report(true);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Report(&mock_data);
}

TEST_F(RequestHandlerImplTest, TestHandlerDisabledReport) {
  ::testing::NiceMock<MockReportData> mock_data;
  EXPECT_CALL(mock_data, GetResponseHeaders()).Times(0);
  EXPECT_CALL(mock_data, GetReportInfo(_)).Times(0);

  // Report should NOT be called.
  EXPECT_CALL(*mock_client_, Report(_)).Times(0);

  ServiceConfig legacy;
  legacy.set_enable_mixer_report(false);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Report(&mock_data);
}

}  // namespace http
}  // namespace mixer_control
}  // namespace istio
