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
#include "contrib/endpoints/src/api_manager/api_manager_impl.h"
#include "contrib/endpoints/src/api_manager/mock_api_manager_environment.h"
#include "gtest/gtest.h"

using ::testing::_;
using ::testing::Invoke;
using ::testing::Mock;
using ::testing::Return;

using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {

namespace {

const char kServerConfigWithSingleServiceConfig[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "init_service_configs": [
    {
      "service_config_file_full_path": "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_1.json",
      "traffic_percentage": 100
    }
  ]
}
)";

const char kServerConfigWithPartialServiceConfig[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "init_service_configs": [
    {
      "service_config_file_full_path": "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_1.json",
      "traffic_percentage": 80
    },
    {
      "service_config_file_full_path": "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_2.json",
      "traffic_percentage": 20
    }
  ]
}
)";

const char kServerConfigWithPartialServiceConfigFailed[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "init_service_configs": [
    {
      "service_config_file_full_path": "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_1.json",
      "traffic_percentage": 80
    },
    {
      "service_config_file_full_path": "not_found.json",
      "traffic_percentage": 20
    }
  ]
}
)";

const char kServerConfigWithNoServiceConfig[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  }
}
)";

const char kServiceConfig1[] = R"(
{
  "name": "bookstore.test.appspot.com",
  "title": "Bookstore",
  "http": {
    "rules": [
      {
        "selector": "EchoGetMessage",
        "get": "/echo"
      }
    ]
  },
  "usage": {
    "rules": [
      {
        "selector": "EchoGetMessage",
        "allowUnregisteredCalls": true
      }
    ]
  },
  "control": {
    "environment": "servicecontrol.googleapis.com"
  },
  "id": "2017-05-01r0"
}
)";

const char kServiceForStatistics[] =
    "name: \"service-name\"\n"
    "control: {\n"
    "  environment: \"http://127.0.0.1:8081\"\n"
    "}\n";

class ApiManagerTest : public ::testing::Test {
 protected:
  ApiManagerTest() : callback_run_count_(0) {}
  std::shared_ptr<ApiManager> MakeApiManager(
      std::unique_ptr<ApiManagerEnvInterface> env, const char *service_config);
  std::shared_ptr<ApiManager> MakeApiManager(
      std::unique_ptr<ApiManagerEnvInterface> env, const char *service_config,
      const char *server_config);

  void SetUp() {
    callback_run_count_ = 0;
    call_history_.clear();
  }

 protected:
  std::vector<std::string> call_history_;
  int callback_run_count_;

 private:
  ApiManagerFactory factory_;
};

std::shared_ptr<ApiManager> ApiManagerTest::MakeApiManager(
    std::unique_ptr<ApiManagerEnvInterface> env, const char *service_config) {
  return factory_.CreateApiManager(std::move(env), service_config, "");
}

std::shared_ptr<ApiManager> ApiManagerTest::MakeApiManager(
    std::unique_ptr<ApiManagerEnvInterface> env, const char *service_config,
    const char *server_config) {
  return factory_.CreateApiManager(std::move(env), service_config,
                                   server_config);
}

TEST_F(ApiManagerTest, EnvironmentLogging) {
  MockApiManagerEnvironment env;

  ::testing::InSequence s;
  EXPECT_CALL(env, Log(ApiManagerEnvInterface::LogLevel::DEBUG, "debug log"));
  EXPECT_CALL(env, Log(ApiManagerEnvInterface::LogLevel::INFO, "info log"));
  EXPECT_CALL(env,
              Log(ApiManagerEnvInterface::LogLevel::WARNING, "warning log"));
  EXPECT_CALL(env, Log(ApiManagerEnvInterface::LogLevel::ERROR, "error log"));

  env.LogDebug("debug log");
  env.LogInfo("info log");
  env.LogWarning("warning log");
  env.LogError("error log");
}

TEST_F(ApiManagerTest, CorrectStatistics) {
  std::unique_ptr<ApiManagerEnvInterface> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(
          MakeApiManager(std::move(env), kServiceForStatistics)));
  EXPECT_TRUE(api_manager);
  EXPECT_TRUE(api_manager->Enabled());
  api_manager->Init();
  ApiManagerStatistics statistics;
  api_manager->GetStatistics(&statistics);
  const service_control::Statistics &service_control_stat =
      statistics.service_control_statistics;
  EXPECT_EQ(0, service_control_stat.total_called_checks);
  EXPECT_EQ(0, service_control_stat.send_checks_by_flush);
  EXPECT_EQ(0, service_control_stat.send_checks_in_flight);
  EXPECT_EQ(0, service_control_stat.total_called_reports);
  EXPECT_EQ(0, service_control_stat.send_reports_by_flush);
  EXPECT_EQ(0, service_control_stat.send_reports_in_flight);
  EXPECT_EQ(0, service_control_stat.send_report_operations);
}

TEST_F(ApiManagerTest, InitializedOnApiManagerInstanceCreation) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  EXPECT_CALL(*(env.get()), DoRunHTTPRequest(_)).Times(0);

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(
          MakeApiManager(std::move(env), kServiceConfig1,
                         kServerConfigWithSingleServiceConfig)));

  EXPECT_TRUE(api_manager);
  EXPECT_TRUE(api_manager->Enabled());

  auto service = api_manager->SelectService();
  EXPECT_TRUE(service);
  EXPECT_EQ("bookstore.test.appspot.com", service->service_name());
  EXPECT_EQ("2017-05-01r0", service->service().id());

  api_manager->Init();

  EXPECT_TRUE(api_manager->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager->service("2017-05-01r0").id());

  service = api_manager->SelectService();
  EXPECT_TRUE(service);
  EXPECT_EQ("bookstore.test.appspot.com", service->service_name());
  EXPECT_EQ("2017-05-01r0", service->service().id());
}

TEST_F(ApiManagerTest, InitializedByConfigManager) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), "", kServerConfigWithSingleServiceConfig)));

  EXPECT_TRUE(api_manager);
  EXPECT_TRUE(api_manager->Enabled());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager->service_name());
  EXPECT_EQ("2017-05-01r0", api_manager->service("2017-05-01r0").id());

  api_manager->Init();

  EXPECT_TRUE(api_manager->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager->service("2017-05-01r0").id());

  auto service = api_manager->SelectService();
  EXPECT_TRUE(service);
  EXPECT_EQ("bookstore.test.appspot.com", service->service_name());
  EXPECT_EQ("2017-05-01r0", service->service().id());
}

TEST_F(ApiManagerTest, kServerConfigWithPartialServiceConfig) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), "", kServerConfigWithPartialServiceConfig)));

  EXPECT_TRUE(api_manager);
  EXPECT_TRUE(api_manager->Enabled());
  EXPECT_EQ("bookstore.test.appspot.com", api_manager->service_name());
  EXPECT_EQ("2017-05-01r0", api_manager->service("2017-05-01r0").id());
  EXPECT_EQ("2017-05-01r1", api_manager->service("2017-05-01r1").id());

  api_manager->Init();

  EXPECT_TRUE(api_manager->Enabled());
  EXPECT_EQ("2017-05-01r0", api_manager->service("2017-05-01r0").id());
  EXPECT_EQ("2017-05-01r1", api_manager->service("2017-05-01r1").id());

  std::unordered_map<std::string, int> counter = {{"2017-05-01r0", 0},
                                                  {"2017-05-01r1", 0}};
  for (int i = 0; i < 100; i++) {
    auto service = api_manager->SelectService();
    EXPECT_TRUE(service);
    EXPECT_EQ("bookstore.test.appspot.com", service->service_name());
    counter[service->service().id()]++;
  }
  EXPECT_EQ(80, counter["2017-05-01r0"]);
  EXPECT_EQ(20, counter["2017-05-01r1"]);
}

TEST_F(ApiManagerTest, kServerConfigWithInvaludServiceConfig) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), "", kServerConfigWithPartialServiceConfigFailed)));

  EXPECT_TRUE(api_manager);
  EXPECT_FALSE(api_manager->Enabled());

  api_manager->Init();

  EXPECT_FALSE(api_manager->Enabled());
}

TEST_F(ApiManagerTest, kServerConfigServiceConfigNotSpecifed) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), "", kServerConfigWithNoServiceConfig)));

  EXPECT_TRUE(api_manager);
  EXPECT_FALSE(api_manager->Enabled());

  api_manager->Init();

  EXPECT_FALSE(api_manager->Enabled());
}

}  // namespace

}  // namespace api_manager
}  // namespace google
