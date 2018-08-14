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

#include "gtest/gtest.h"
#include "proxy/src/istio/control/mock_mixer_client.h"
#include "proxy/src/istio/control/tcp/client_context.h"
#include "proxy/src/istio/control/tcp/controller_impl.h"
#include "proxy/src/istio/control/tcp/mock_check_data.h"
#include "proxy/src/istio/control/tcp/mock_report_data.h"

using ::google::protobuf::util::Status;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::config::client::TcpClientConfig;
using ::istio::mixerclient::CancelFunc;
using ::istio::mixerclient::CheckDoneFunc;
using ::istio::mixerclient::CheckResponseInfo;
using ::istio::mixerclient::DoneFunc;
using ::istio::mixerclient::MixerClient;
using ::istio::mixerclient::TransportCheckFunc;
using ::istio::quota_config::Requirement;

using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace control {
namespace tcp {

class RequestHandlerImplTest : public ::testing::Test {
 public:
  void SetUp() {
    auto map1 = client_config_.mutable_mixer_attributes()->mutable_attributes();
    (*map1)["key1"].set_string_value("value1");

    auto quota = client_config_.mutable_connection_quota_spec()
                     ->add_rules()
                     ->add_quotas();
    quota->set_quota("quota");
    quota->set_charge(5);

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
  EXPECT_CALL(mock_data, GetPrincipal(_, _)).Times(2);

  // Check should not be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _)).Times(0);

  client_config_.set_disable_check_calls(true);
  auto handler = controller_->CreateRequestHandler();
  handler->Check(&mock_data, [](const CheckResponseInfo& info) {
    EXPECT_TRUE(info.response_status.ok());
  });
}

TEST_F(RequestHandlerImplTest, TestHandlerCheck) {
  ::testing::NiceMock<MockCheckData> mock_data;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetPrincipal(_, _)).Times(2);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke([](const Attributes& attributes,
                          const std::vector<Requirement>& quotas,
                          TransportCheckFunc transport,
                          CheckDoneFunc on_done) -> CancelFunc {
        auto map = attributes.attributes();
        EXPECT_EQ(map["key1"].string_value(), "value1");
        EXPECT_EQ(quotas.size(), 1);
        EXPECT_EQ(quotas[0].quota, "quota");
        EXPECT_EQ(quotas[0].charge, 5);
        return nullptr;
      }));

  auto handler = controller_->CreateRequestHandler();
  handler->Check(&mock_data, nullptr);
}

TEST_F(RequestHandlerImplTest, TestHandlerReport) {
  ::testing::NiceMock<MockReportData> mock_data;
  EXPECT_CALL(mock_data, GetDestinationIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetDestinationUID(_)).Times(1);
  EXPECT_CALL(mock_data, GetReportInfo(_)).Times(1);

  // Report should be called.
  EXPECT_CALL(*mock_client_, Report(_)).Times(1);

  auto handler = controller_->CreateRequestHandler();
  handler->Report(&mock_data, ReportData::CONTINUE);
}

}  // namespace tcp
}  // namespace control
}  // namespace istio
