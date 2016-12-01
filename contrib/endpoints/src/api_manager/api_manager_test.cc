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
#include "include/api_manager/api_manager.h"
#include "gtest/gtest.h"
#include "src/api_manager/mock_api_manager_environment.h"

#include <memory>

namespace google {
namespace api_manager {

namespace {

class ApiManagerTest : public ::testing::Test {
 protected:
  std::shared_ptr<ApiManager> MakeApiManager(
      std::unique_ptr<ApiManagerEnvInterface> env, const char *service_config);

 private:
  ApiManagerFactory factory_;
};

std::shared_ptr<ApiManager> ApiManagerTest::MakeApiManager(
    std::unique_ptr<ApiManagerEnvInterface> env, const char *service_config) {
  return factory_.GetOrCreateApiManager(std::move(env), service_config, "");
}

TEST_F(ApiManagerTest, EmptyConfig) {
  MockApiManagerEnvironment *raw_env = new MockApiManagerEnvironment();
  std::unique_ptr<ApiManagerEnvInterface> env(raw_env);
  ASSERT_FALSE(MakeApiManager(std::move(env), ""));
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

const char service_one[] = "name: \"service-one\"\n";
const char service_two[] = "name: \"service-two\"\n";

TEST_F(ApiManagerTest, SameServiceNameYieldsSameApiManager) {
  std::unique_ptr<ApiManagerEnvInterface> env_one(
      new ::testing::NiceMock<MockApiManagerEnvironment>());
  std::unique_ptr<ApiManagerEnvInterface> env_two(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManager> esp1(
      MakeApiManager(std::move(env_one), service_one));
  std::shared_ptr<ApiManager> esp2(
      MakeApiManager(std::move(env_two), service_one));

  EXPECT_TRUE(esp1);
  EXPECT_TRUE(esp2);
  ASSERT_EQ(esp1.get(), esp2.get());
}

const char kServiceForStatistics[] =
    "name: \"service-name\"\n"
    "control: {\n"
    "  environment: \"http://127.0.0.1:8081\"\n"
    "}\n";

TEST_F(ApiManagerTest, CorrectStatistics) {
  std::unique_ptr<ApiManagerEnvInterface> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManager> api_manager(
      MakeApiManager(std::move(env), kServiceForStatistics));
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

TEST_F(ApiManagerTest, DifferentServiceNameYieldsDifferentApiManager) {
  std::unique_ptr<ApiManagerEnvInterface> env_one(
      new ::testing::NiceMock<MockApiManagerEnvironment>());
  std::unique_ptr<ApiManagerEnvInterface> env_two(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManager> esp1(
      MakeApiManager(std::move(env_one), service_one));
  std::shared_ptr<ApiManager> esp2(
      MakeApiManager(std::move(env_two), service_two));

  EXPECT_TRUE(esp1);
  EXPECT_TRUE(esp2);
  ASSERT_NE(esp1.get(), esp2.get());
}

TEST_F(ApiManagerTest, CreateAfterExpirationWorks) {
  std::unique_ptr<ApiManagerEnvInterface> env_one(
      new ::testing::NiceMock<MockApiManagerEnvironment>());
  std::unique_ptr<ApiManagerEnvInterface> env_two(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManager> esp1(
      MakeApiManager(std::move(env_one), service_one));
  EXPECT_TRUE(esp1);
  std::weak_ptr<ApiManager> esp1_weak(esp1);
  ASSERT_FALSE(esp1_weak.expired());
  esp1.reset();
  ASSERT_TRUE(esp1_weak.expired());

  std::shared_ptr<ApiManager> esp2(
      MakeApiManager(std::move(env_two), service_one));
  ASSERT_TRUE(esp2);
}

}  // namespace

}  // namespace api_manager
}  // namespace google
