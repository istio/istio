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
  "service_config_rollout": {
    traffic_percentages: {
      "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_1.json": 100
    }
  }
}
)";

const char kServerConfigWithPartialServiceConfig[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "service_config_rollout": {
    traffic_percentages: {
      "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_1.json": 80,
      "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_2.json": 20,
    }
  }
}
)";

const char kServerConfigWithPartialServiceConfigFailed[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "service_config_rollout": {
    traffic_percentages: {
      "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_1.json": 80,
      "not_found.json": 20,
    }
  }
}
)";

const char kServerConfigWithManagedRolloutStrategy[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "service_config_rollout": {
    traffic_percentages: {
      "contrib/endpoints/src/api_manager/testdata/bookstore_service_config_1.json": 100
    }
  },
  "rollout_strategy": "managed"
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

const char kServiceConfig2[] = R"(
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
  "id": "2017-05-01r1"
}
)";

const char kRolloutsResponse1[] = R"(
{
  "rollouts": [
    {
      "rolloutId": "2017-05-01r0",
      "createTime": "2017-05-01T22:40:09.884Z",
      "createdBy": "test_user@google.com",
      "status": "SUCCESS",
      "trafficPercentStrategy": {
        "percentages": {
          "2017-05-01r1": 100
        }
      },
      "serviceName": "service_name_from_server_config"
    }
  ]
}
)";

const char kServiceForStatistics[] =
    "name: \"service-name\"\n"
    "control: {\n"
    "  environment: \"http://127.0.0.1:8081\"\n"
    "}\n";

// Simulate periodic timer event on creation
class MockPeriodicTimer : public PeriodicTimer {
 public:
  MockPeriodicTimer() {}
  MockPeriodicTimer(std::function<void()> continuation)
      : continuation_(continuation) {
    continuation_();
  }

  virtual ~MockPeriodicTimer() {}
  void Stop(){};

 private:
  std::function<void()> continuation_;
};

class MockTimerApiManagerEnvironment : public MockApiManagerEnvironment {
 public:
  MOCK_METHOD2(Log, void(LogLevel, const char *));
  MOCK_METHOD1(MakeTag, void *(std::function<void(bool)>));

  virtual std::unique_ptr<PeriodicTimer> StartPeriodicTimer(
      std::chrono::milliseconds interval, std::function<void()> continuation) {
    return std::unique_ptr<PeriodicTimer>(new MockPeriodicTimer(continuation));
  }

  MOCK_METHOD1(DoRunHTTPRequest, void(HTTPRequest *));
  MOCK_METHOD1(DoRunGRPCRequest, void(GRPCRequest *));
  virtual void RunHTTPRequest(std::unique_ptr<HTTPRequest> req) {
    DoRunHTTPRequest(req.get());
  }
  virtual void RunGRPCRequest(std::unique_ptr<GRPCRequest> req) {
    DoRunGRPCRequest(req.get());
  }

 private:
  std::unique_ptr<PeriodicTimer> periodic_timer_;
};

class ApiManagerTest : public ::testing::Test {
 protected:
  ApiManagerTest() : callback_run_count_(0) {}
  std::shared_ptr<ApiManager> MakeApiManager(
      std::unique_ptr<ApiManagerEnvInterface> env, const char *server_config);

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
    std::unique_ptr<ApiManagerEnvInterface> env, const char *server_config) {
  return factory_.CreateApiManager(std::move(env), server_config);
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

TEST_F(ApiManagerTest, InvalidServerConfig) {
  std::unique_ptr<ApiManagerEnvInterface> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(
          MakeApiManager(std::move(env), kServiceForStatistics)));
  EXPECT_TRUE(api_manager);
  EXPECT_FALSE(api_manager->Enabled());
  api_manager->Init();
  EXPECT_FALSE(api_manager->Enabled());
}

TEST_F(ApiManagerTest, CorrectStatistics) {
  std::unique_ptr<ApiManagerEnvInterface> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), kServerConfigWithSingleServiceConfig)));
  EXPECT_OK(api_manager->LoadServiceRollouts());

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
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), kServerConfigWithSingleServiceConfig)));
  EXPECT_OK(api_manager->LoadServiceRollouts());

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
          std::move(env), kServerConfigWithSingleServiceConfig)));
  EXPECT_OK(api_manager->LoadServiceRollouts());

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

TEST_F(ApiManagerTest, ManagedRolloutStrategy) {
  std::unique_ptr<MockTimerApiManagerEnvironment> env(
      new ::testing::NiceMock<MockTimerApiManagerEnvironment>());

  EXPECT_CALL(*env.get(), DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponse1);
      }))
      .WillOnce(Invoke([this](HTTPRequest *req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "bookstore.test.appspot.com/configs/2017-05-01r1",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig2);
      }));

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), kServerConfigWithManagedRolloutStrategy)));
  EXPECT_OK(api_manager->LoadServiceRollouts());

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
  EXPECT_EQ("2017-05-01r1", service->service().id());
}

TEST_F(ApiManagerTest, ServerConfigWithPartialServiceConfig) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), kServerConfigWithPartialServiceConfig)));
  EXPECT_OK(api_manager->LoadServiceRollouts());

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

TEST_F(ApiManagerTest, ServerConfigWithInvalidServiceConfig) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(MakeApiManager(
          std::move(env), kServerConfigWithPartialServiceConfigFailed)));
  EXPECT_FALSE(api_manager->LoadServiceRollouts().ok());

  EXPECT_TRUE(api_manager);
  EXPECT_FALSE(api_manager->Enabled());

  api_manager->Init();

  EXPECT_FALSE(api_manager->Enabled());
}

TEST_F(ApiManagerTest, ServerConfigServiceConfigNotSpecifed) {
  std::unique_ptr<MockApiManagerEnvironment> env(
      new ::testing::NiceMock<MockApiManagerEnvironment>());

  std::shared_ptr<ApiManagerImpl> api_manager(
      std::dynamic_pointer_cast<ApiManagerImpl>(
          MakeApiManager(std::move(env), kServerConfigWithNoServiceConfig)));
  EXPECT_FALSE(api_manager->LoadServiceRollouts().ok());

  EXPECT_TRUE(api_manager);
  EXPECT_FALSE(api_manager->Enabled());

  api_manager->Init();

  EXPECT_FALSE(api_manager->Enabled());
}

}  // namespace

}  // namespace api_manager
}  // namespace google
