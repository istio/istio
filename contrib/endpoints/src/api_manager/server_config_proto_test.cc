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
#include "gtest/gtest.h"

#include <fstream>
#include <sstream>
#include <string>

#include "contrib/endpoints/src/api_manager/proto/server_config.pb.h"
#include "google/protobuf/text_format.h"

using ::google::protobuf::TextFormat;

using ::google::api_manager::proto::CheckAggregatorConfig;
using ::google::api_manager::proto::ReportAggregatorConfig;
using ::google::api_manager::proto::ServerConfig;
using ::google::api_manager::proto::ServiceControlConfig;

// Trivial tests that instantiate server config protos
// to make sure each field is set correctly.
namespace google {
namespace api_manager {

namespace {

const char kServerConfig[] = R"(
service_control_config {
  force_disable : false,
  url_override : "http://chemist_override"

  check_aggregator_config {
    cache_entries: 1000
    flush_interval_ms: 10
    response_expiration_ms: 20
  }

  report_aggregator_config {
    cache_entries: 1020
    flush_interval_ms: 15
  }
}

metadata_server_config {
  enabled: true
  url: "http://metadata_server"
}

google_authentication_secret: "{"
                                "The client secret goes here."
                              "}"

cloud_tracing_config {
  force_disable: false,
  url_override: "http://cloud_tracing_override"
}

api_authentication_config {
  force_disable: true
}

experimental {
  disable_log_status: false
}

)";

TEST(ServerConfigProto, ServerConfigFromString) {
  ServerConfig server_config;
  ASSERT_TRUE(TextFormat::ParseFromString(kServerConfig, &server_config));

  // Check service_control_config
  EXPECT_EQ(false, server_config.service_control_config().force_disable());
  EXPECT_EQ("http://chemist_override",
            server_config.service_control_config().url_override());

  EXPECT_EQ(1000, server_config.service_control_config()
                      .check_aggregator_config()
                      .cache_entries());
  EXPECT_EQ(10, server_config.service_control_config()
                    .check_aggregator_config()
                    .flush_interval_ms());
  EXPECT_EQ(20, server_config.service_control_config()
                    .check_aggregator_config()
                    .response_expiration_ms());
  EXPECT_EQ(1020, server_config.service_control_config()
                      .report_aggregator_config()
                      .cache_entries());
  EXPECT_EQ(15, server_config.service_control_config()
                    .report_aggregator_config()
                    .flush_interval_ms());

  // Check metadata_server_config
  EXPECT_EQ(true, server_config.metadata_server_config().enabled());
  EXPECT_EQ("http://metadata_server",
            server_config.metadata_server_config().url());

  // Check google_authentication_secret
  EXPECT_EQ("{The client secret goes here.}",
            server_config.google_authentication_secret());

  // Check cloud_tracing_config
  EXPECT_EQ(false, server_config.cloud_tracing_config().force_disable());
  EXPECT_EQ("http://cloud_tracing_override",
            server_config.cloud_tracing_config().url_override());

  // Check api_authentication_config
  EXPECT_EQ(true, server_config.api_authentication_config().force_disable());

  // Check disable_log_status
  EXPECT_EQ(false, server_config.experimental().disable_log_status());
}

TEST(ServerConfigProto, ValidateSampleServerConfig) {
  const char kSampleServerConfigPath[] =
      "contrib/endpoints/src/api_manager/proto/sample_server_config.pb.txt";

  std::ifstream ifs(kSampleServerConfigPath);
  ASSERT_TRUE(ifs) << "Failed to open the sample server config file."
                   << std::endl;

  std::ostringstream ss;
  ss << ifs.rdbuf();

  auto sample_server_config = ss.str();
  ASSERT_FALSE(sample_server_config.empty())
      << "Sample server config must not be empty" << std::endl;

  ServerConfig server_config;
  EXPECT_TRUE(TextFormat::ParseFromString(ss.str(), &server_config));

  // Check disable_log_status
  EXPECT_EQ(true, server_config.experimental().disable_log_status());
}

TEST(ServerConfigProto, ServerConfigSetManually) {
  ServerConfig server_config;

  ServiceControlConfig* service_control_config =
      server_config.mutable_service_control_config();

  CheckAggregatorConfig* check_config =
      service_control_config->mutable_check_aggregator_config();
  check_config->set_cache_entries(2000);
  check_config->set_flush_interval_ms(20);
  check_config->set_response_expiration_ms(23);

  ReportAggregatorConfig* report_config =
      service_control_config->mutable_report_aggregator_config();
  report_config->set_cache_entries(2500);
  report_config->set_flush_interval_ms(25);

  ASSERT_EQ(2000, server_config.service_control_config()
                      .check_aggregator_config()
                      .cache_entries());
  ASSERT_EQ(20, server_config.service_control_config()
                    .check_aggregator_config()
                    .flush_interval_ms());
  ASSERT_EQ(23, server_config.service_control_config()
                    .check_aggregator_config()
                    .response_expiration_ms());

  ASSERT_EQ(2500, server_config.service_control_config()
                      .report_aggregator_config()
                      .cache_entries());
  ASSERT_EQ(25, server_config.service_control_config()
                    .report_aggregator_config()
                    .flush_interval_ms());
}

}  // namespace

}  // namespace api_manager
}  // namespace google
