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
