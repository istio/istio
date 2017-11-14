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
using ::istio::mixer::v1::config::client::TcpClientConfig;
using ::istio::mixer_client::MixerClient;

using ::testing::_;

namespace istio {
namespace mixer_control {
namespace tcp {

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
  TcpClientConfig client_config_;
  ::testing::NiceMock<MockMixerClient>* mock_client_;
  std::unique_ptr<Controller> controller_;
};

TEST_F(RequestHandlerImplTest, TestHandlerDisabledCheck) {
  ::testing::NiceMock<MockCheckData> mock_data;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should not be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _)).Times(0);

  client_config_.set_disable_check_calls(true);
  auto handler = controller_->CreateRequestHandler();
  handler->Check(&mock_data,
                 [](const Status& status) { EXPECT_TRUE(status.ok()); });
}

TEST_F(RequestHandlerImplTest, TestHandlerCheck) {
  ::testing::NiceMock<MockCheckData> mock_data;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _)).Times(1);

  auto handler = controller_->CreateRequestHandler();
  handler->Check(&mock_data, nullptr);
}

TEST_F(RequestHandlerImplTest, TestHandlerReport) {
  ::testing::NiceMock<MockReportData> mock_data;
  EXPECT_CALL(mock_data, GetDestinationIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetReportInfo(_)).Times(1);

  // Report should be called.
  EXPECT_CALL(*mock_client_, Report(_)).Times(1);

  auto handler = controller_->CreateRequestHandler();
  handler->Report(&mock_data);
}

}  // namespace tcp
}  // namespace mixer_control
}  // namespace istio
