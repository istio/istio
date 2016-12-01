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
#include "gtest/gtest.h"

#include <fstream>
#include <sstream>
#include <string>

#include "google/protobuf/text_format.h"
#include "src/api_manager/proto/server_config.pb.h"

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
      "src/api_manager/proto/sample_server_config.pb.txt";

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
