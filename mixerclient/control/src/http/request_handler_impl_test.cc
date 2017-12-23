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
#include "control/src/attribute_names.h"
#include "control/src/mock_mixer_client.h"
#include "controller_impl.h"
#include "google/protobuf/text_format.h"
#include "gtest/gtest.h"
#include "mock_check_data.h"
#include "mock_report_data.h"

using ::google::protobuf::TextFormat;
using ::google::protobuf::util::Status;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::config::client::ServiceConfig;
using ::istio::mixer::v1::config::client::HttpClientConfig;
using ::istio::mixer_client::CancelFunc;
using ::istio::mixer_client::TransportCheckFunc;
using ::istio::mixer_client::DoneFunc;
using ::istio::mixer_client::MixerClient;
using ::istio::quota::Requirement;

using ::testing::_;
using ::testing::Invoke;

namespace istio {
namespace mixer_control {
namespace http {

// The default client config
const char kDefaultClientConfig[] = R"(
service_configs {
  key: ":default"
  value {
    mixer_attributes {
      attributes {
        key: "route0-key"
        value {
          string_value: "route0-value"
        }
      }
    }
  }
}
default_destination_service: ":default"
mixer_attributes {
  attributes {
    key: "global-key"
    value {
      string_value: "global-value"
    }
  }
}
)";

// The client config with empty service map.
const char kEmptyClientConfig[] = R"(
forward_attributes {
  attributes {
    key: "source-key"
    value {
      string_value: "source-value"
    }
  }
}
)";

class RequestHandlerImplTest : public ::testing::Test {
 public:
  void SetUp() {
    // add a legacy quota
    legacy_quotas_.push_back({"legacy-quota", 10});

    SetUpMockController(kDefaultClientConfig);
  }

  void SetUpMockController(const std::string& config_text) {
    ASSERT_TRUE(TextFormat::ParseFromString(config_text, &client_config_));

    mock_client_ = new ::testing::NiceMock<MockMixerClient>;
    client_context_ = std::make_shared<ClientContext>(
        std::unique_ptr<MixerClient>(mock_client_), client_config_,
        legacy_quotas_);
    controller_ =
        std::unique_ptr<Controller>(new ControllerImpl(client_context_));
  }

  void SetServiceConfig(const std::string& name, const ServiceConfig& config) {
    (*client_config_.mutable_service_configs())[name] = config;
  }

  std::shared_ptr<ClientContext> client_context_;
  HttpClientConfig client_config_;
  std::vector<Requirement> legacy_quotas_;
  ::testing::NiceMock<MockMixerClient>* mock_client_;
  std::unique_ptr<Controller> controller_;
};

TEST_F(RequestHandlerImplTest, TestHandlerDisabledCheckReport) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  // Not to extract attributes since both Check and Report are disabled.
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(0);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(0);

  // Check should NOT be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _)).Times(0);

  ServiceConfig legacy;
  legacy.set_disable_check_calls(true);
  legacy.set_disable_report_calls(true);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr,
                 [](const Status& status) { EXPECT_TRUE(status.ok()); });
}

TEST_F(RequestHandlerImplTest, TestHandlerDisabledCheck) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  // Report is enabled so Attributes are extracted.
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should NOT be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _)).Times(0);

  ServiceConfig legacy;
  legacy.set_disable_check_calls(true);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr,
                 [](const Status& status) { EXPECT_TRUE(status.ok()); });
}

TEST_F(RequestHandlerImplTest, TestLegacyRoute) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke([](const Attributes& attributes,
                          const std::vector<Requirement>& quotas,
                          TransportCheckFunc transport,
                          DoneFunc on_done) -> CancelFunc {
        auto map = attributes.attributes();
        EXPECT_EQ(map["global-key"].string_value(), "global-value");
        EXPECT_EQ(map["legacy-key"].string_value(), "legacy-value");
        EXPECT_EQ(quotas.size(), 1);
        EXPECT_EQ(quotas[0].quota, "legacy-quota");
        EXPECT_EQ(quotas[0].charge, 10);
        return nullptr;
      }));

  ServiceConfig legacy;
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto map2 = legacy.mutable_mixer_attributes()->mutable_attributes();
  (*map2)["legacy-key"].set_string_value("legacy-value");

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestDefaultRouteAttributes) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke([](const Attributes& attributes,
                          const std::vector<Requirement>& quotas,
                          TransportCheckFunc transport,
                          DoneFunc on_done) -> CancelFunc {
        auto map = attributes.attributes();
        EXPECT_EQ(map["global-key"].string_value(), "global-value");
        EXPECT_EQ(map["route0-key"].string_value(), "route0-value");
        return nullptr;
      }));

  // destionation.server is empty, will use default one
  Controller::PerRouteConfig config;
  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestRouteAttributes) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  ServiceConfig route_config;
  auto map3 = route_config.mutable_mixer_attributes()->mutable_attributes();
  (*map3)["route1-key"].set_string_value("route1-value");
  SetServiceConfig("route1", route_config);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke([](const Attributes& attributes,
                          const std::vector<Requirement>& quotas,
                          TransportCheckFunc transport,
                          DoneFunc on_done) -> CancelFunc {
        auto map = attributes.attributes();
        EXPECT_EQ(map["global-key"].string_value(), "global-value");
        EXPECT_EQ(map["route1-key"].string_value(), "route1-value");
        return nullptr;
      }));

  // destionation.server is empty, will use default one
  Controller::PerRouteConfig config;
  config.destination_service = "route1";
  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestDefaultRouteQuota) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;

  ServiceConfig route_config;
  auto quota = route_config.add_quota_spec()->add_rules()->add_quotas();
  quota->set_quota("route0-quota");
  quota->set_charge(10);
  SetServiceConfig(":default", route_config);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke([](const Attributes& attributes,
                          const std::vector<Requirement>& quotas,
                          TransportCheckFunc transport,
                          DoneFunc on_done) -> CancelFunc {
        auto map = attributes.attributes();
        EXPECT_EQ(map["global-key"].string_value(), "global-value");
        EXPECT_EQ(quotas.size(), 2);
        EXPECT_EQ(quotas[0].quota, "legacy-quota");
        EXPECT_EQ(quotas[0].charge, 10);
        EXPECT_EQ(quotas[1].quota, "route0-quota");
        EXPECT_EQ(quotas[1].charge, 10);
        return nullptr;
      }));

  // destionation.server is empty, will use default one
  Controller::PerRouteConfig config;
  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestDefaultRouteApiSpec) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  EXPECT_CALL(mock_data, FindHeaderByType(_, _))
      .WillRepeatedly(
          Invoke([](CheckData::HeaderType type, std::string* value) -> bool {
            if (type == CheckData::HEADER_PATH) {
              *value = "/books/120";
              return true;
            }
            if (type == CheckData::HEADER_METHOD) {
              *value = "GET";
              return true;
            }
            return false;
          }));

  ServiceConfig route_config;
  auto api_spec = route_config.add_http_api_spec();
  auto map1 = api_spec->mutable_attributes()->mutable_attributes();
  (*map1)["api.name"].set_string_value("test-name");
  auto pattern = api_spec->add_patterns();
  auto map2 = pattern->mutable_attributes()->mutable_attributes();
  (*map2)["api.operation"].set_string_value("test-method");
  pattern->set_http_method("GET");
  pattern->set_uri_template("/books/*");
  SetServiceConfig(":default", route_config);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke([](const Attributes& attributes,
                          const std::vector<Requirement>& quotas,
                          TransportCheckFunc transport,
                          DoneFunc on_done) -> CancelFunc {
        auto map = attributes.attributes();
        EXPECT_EQ(map["global-key"].string_value(), "global-value");
        EXPECT_EQ(map["api.name"].string_value(), "test-name");
        EXPECT_EQ(map["api.operation"].string_value(), "test-method");
        return nullptr;
      }));

  // destionation.server is empty, will use default one
  Controller::PerRouteConfig config;
  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestHandlerCheck) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  EXPECT_CALL(mock_data, GetSourceIpPort(_, _)).Times(1);
  EXPECT_CALL(mock_data, GetSourceUser(_)).Times(1);

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _)).Times(1);

  ServiceConfig legacy;
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestDefaultApiKey) {
  ::testing::NiceMock<MockCheckData> mock_data;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  EXPECT_CALL(mock_data, FindQueryParameter(_, _))
      .WillRepeatedly(
          Invoke([](const std::string& name, std::string* value) -> bool {
            if (name == "key") {
              *value = "test-api-key";
              return true;
            }
            return false;
          }));

  // Check should be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _))
      .WillOnce(Invoke([](const Attributes& attributes,
                          const std::vector<Requirement>& quotas,
                          TransportCheckFunc transport,
                          DoneFunc on_done) -> CancelFunc {
        auto map = attributes.attributes();
        EXPECT_EQ(map[AttributeName::kRequestApiKey].string_value(),
                  "test-api-key");
        return nullptr;
      }));

  // destionation.server is empty, will use default one
  Controller::PerRouteConfig config;
  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_data, &mock_header, nullptr, nullptr);
}

TEST_F(RequestHandlerImplTest, TestHandlerReport) {
  ::testing::NiceMock<MockReportData> mock_data;
  EXPECT_CALL(mock_data, GetResponseHeaders()).Times(1);
  EXPECT_CALL(mock_data, GetReportInfo(_)).Times(1);

  // Report should be called.
  EXPECT_CALL(*mock_client_, Report(_)).Times(1);

  ServiceConfig legacy;
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
  legacy.set_disable_report_calls(true);
  Controller::PerRouteConfig config;
  config.legacy_config = &legacy;

  auto handler = controller_->CreateRequestHandler(config);
  handler->Report(&mock_data);
}

TEST_F(RequestHandlerImplTest, TestEmptyConfig) {
  SetUpMockController(kEmptyClientConfig);

  ::testing::NiceMock<MockCheckData> mock_check;
  ::testing::NiceMock<MockHeaderUpdate> mock_header;
  // Not to extract attributes since both Check and Report are disabled.
  EXPECT_CALL(mock_check, GetSourceIpPort(_, _)).Times(0);
  EXPECT_CALL(mock_check, GetSourceUser(_)).Times(0);

  // Attributes is forwarded.
  EXPECT_CALL(mock_header, AddIstioAttributes(_))
      .WillOnce(Invoke([](const std::string& data) {
        Attributes forwarded_attr;
        EXPECT_TRUE(forwarded_attr.ParseFromString(data));
        auto map = forwarded_attr.attributes();
        EXPECT_EQ(map["source-key"].string_value(), "source-value");
      }));

  // Check should NOT be called.
  EXPECT_CALL(*mock_client_, Check(_, _, _, _)).Times(0);

  ::testing::NiceMock<MockReportData> mock_report;
  EXPECT_CALL(mock_report, GetResponseHeaders()).Times(0);
  EXPECT_CALL(mock_report, GetReportInfo(_)).Times(0);

  // Report should NOT be called.
  EXPECT_CALL(*mock_client_, Report(_)).Times(0);

  Controller::PerRouteConfig config;
  auto handler = controller_->CreateRequestHandler(config);
  handler->Check(&mock_check, &mock_header, nullptr,
                 [](const Status& status) { EXPECT_TRUE(status.ok()); });
  handler->Report(&mock_report);
}

}  // namespace http
}  // namespace mixer_control
}  // namespace istio
