/* Copyright 2017 Google Inc. All Rights Reserved.
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
#include "contrib/endpoints/src/api_manager/config_manager.h"

#include "contrib/endpoints/src/api_manager/config.h"
#include "contrib/endpoints/src/api_manager/context/global_context.h"
#include "contrib/endpoints/src/api_manager/mock_api_manager_environment.h"

using ::testing::_;
using ::testing::Invoke;
using ::testing::Mock;
using ::testing::Return;

using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {

namespace {

const char kServerConfigWithServiceName[] = R"(
{
  "google_authentication_secret": "{}",
  "metadata_server_config": {
    "enabled": true,
    "url": "http://localhost"
  },
  "service_control_config": {
    "report_aggregator_config": {
      "cache_entries": 10000,
      "flush_interval_ms": 1000001232
    },
    "quota_aggregator_config": {
      "cache_entries": 300000,
      "refresh_interval_ms": 1000
    }
  },
  "service_name": "service_name_from_server_config",
  "rollout_strategy": "managed"
}
)";

const char kGceMetadataWithServiceNameAndConfigId[] = R"(
{
  "project": {
    "projectId": "test-project"
  },
  "instance": {
    "attributes":{
      "endpoints-service-name": "service_name_from_metadata",
      "endpoints-service-config-id":"2017-05-01r1"
    }
  }
}
)";

const char kServiceConfig1[] = R"(
{
  "name": "bookstore.test.appspot.com",
  "title": "Bookstore",
  "id": "2017-05-01r0"
}
)";

const char kServiceConfig2[] = R"(
{
  "name": "bookstore.test.appspot.com",
  "title": "Bookstore",
  "id": "2017-05-01r1"
}
)";

const char kServiceConfig3[] = R"(
{
  "name": "bookstore.test.appspot.com",
  "title": "Bookstore",
  "id": "2017-05-01r2"
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
          "2017-05-01r0": 100
        }
      },
      "serviceName": "service_name_from_server_config"
    }
  ]
}
)";

const char kRolloutsResponse2[] = R"(
{
  "rollouts": [
    {
      "rolloutId": "2017-05-01r1",
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

const char kRolloutsResponseMultipleServiceConfig[] = R"(
{
  "rollouts": [
    {
      "rolloutId": "2017-05-01r0",
      "createTime": "2017-05-01T22:40:09.884Z",
      "createdBy": "test_user@google.com",
      "status": "FAILED",
      "trafficPercentStrategy": {
        "percentages": {
          "2017-05-01r0": 80,
          "2017-05-01r1": 20
        }
      },
      "serviceName": "service_name_from_server_config"
    }
  ]
}
)";

// Represents a periodic timer created by API Manager's environment.
class MockPeriodicTimer : public PeriodicTimer {
 public:
  MockPeriodicTimer() {}
  MockPeriodicTimer(std::function<void()> continuation)
      : continuation_(continuation) {}

  virtual ~MockPeriodicTimer() {}
  void Stop(){};

  void Run() { continuation_(); }

 private:
  std::function<void()> continuation_;
};

class MockTimerApiManagerEnvironment : public MockApiManagerEnvironment {
 public:
  MOCK_METHOD2(Log, void(LogLevel, const char*));
  MOCK_METHOD1(MakeTag, void*(std::function<void(bool)>));

  virtual std::unique_ptr<PeriodicTimer> StartPeriodicTimer(
      std::chrono::milliseconds interval, std::function<void()> continuation) {
    mock_periodic_timer_ = new MockPeriodicTimer(continuation);
    return std::unique_ptr<PeriodicTimer>(mock_periodic_timer_);
  }

  MOCK_METHOD1(DoRunHTTPRequest, void(HTTPRequest*));
  MOCK_METHOD1(DoRunGRPCRequest, void(GRPCRequest*));
  virtual void RunHTTPRequest(std::unique_ptr<HTTPRequest> req) {
    DoRunHTTPRequest(req.get());
  }
  virtual void RunGRPCRequest(std::unique_ptr<GRPCRequest> req) {
    DoRunGRPCRequest(req.get());
  }

  void RunTimer() { mock_periodic_timer_->Run(); }

 private:
  std::unique_ptr<PeriodicTimer> periodic_timer_;
  MockPeriodicTimer* mock_periodic_timer_;
};

// Both service_name, config_id in server config
class ConfigManagerServiceNameConfigIdTest : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockTimerApiManagerEnvironment>());
    // save the raw pointer of env before calling std::move(env).
    raw_env_ = env_.get();

    global_context_ = std::make_shared<context::GlobalContext>(
        std::move(env_), kServerConfigWithServiceName);

    global_context_->set_service_name("service_name_from_metadata");

    history_.clear();
  }

  std::unique_ptr<MockTimerApiManagerEnvironment> env_;
  MockTimerApiManagerEnvironment* raw_env_;
  std::shared_ptr<context::GlobalContext> global_context_;
  std::vector<std::string> history_;
};

TEST_F(ConfigManagerServiceNameConfigIdTest, RolloutSingleServiceConfig) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponse1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig1);
      }));

  int sequence = 0;
  std::shared_ptr<ConfigManager> config_manager(new ConfigManager(
      global_context_,
      [this, &sequence](const utils::Status& status,
                        const std::vector<std::pair<std::string, int>>& list) {

        ASSERT_EQ(1, list.size());
        ASSERT_EQ(kServiceConfig1, list[0].first);
        ASSERT_EQ(100, list[0].second);
        sequence++;
      }));

  config_manager->Init();
  ASSERT_EQ(0, sequence);
  raw_env_->RunTimer();
  ASSERT_EQ(1, sequence);
}

TEST_F(ConfigManagerServiceNameConfigIdTest,
       RemoteRolloutIDIsSameAsRolloutIDInServerConfig) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponse1);
      }));

  int sequence = 0;
  std::shared_ptr<ConfigManager> config_manager(new ConfigManager(
      global_context_,
      [this, &sequence](const utils::Status& status,
                        const std::vector<std::pair<std::string, int>>& list) {

        ASSERT_EQ(1, list.size());
        ASSERT_EQ(kServiceConfig1, list[0].first);
        ASSERT_EQ(100, list[0].second);
        sequence++;
      }));

  // set rollout_id to 2017-05-01r0 which is same as kRolloutsResponse1
  config_manager->set_current_rollout_id("2017-05-01r0");

  config_manager->Init();
  ASSERT_EQ(0, sequence);
  raw_env_->RunTimer();
  // callback should not be called
  ASSERT_EQ(0, sequence);
}

TEST_F(ConfigManagerServiceNameConfigIdTest, RolloutMultipleServiceConfig) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponseMultipleServiceConfig);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r1",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig2);
      }));

  int sequence = 0;

  std::shared_ptr<ConfigManager> config_manager(new ConfigManager(
      global_context_,
      [this, &sequence](const utils::Status& status,
                        const std::vector<std::pair<std::string, int>>& list) {
        ASSERT_EQ(2, list.size());
        ASSERT_EQ(kServiceConfig1, list[0].first);
        ASSERT_EQ(80, list[0].second);
        ASSERT_EQ(kServiceConfig2, list[1].first);
        ASSERT_EQ(20, list[1].second);
        sequence++;
      }));

  config_manager->Init();
  ASSERT_EQ(0, sequence);
  raw_env_->RunTimer();
  ASSERT_EQ(1, sequence);
}

TEST_F(ConfigManagerServiceNameConfigIdTest,
       RolloutMultipleServiceConfigPartiallyFailedThenSucceededNextTimerEvent) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponseMultipleServiceConfig);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r1",
            req->url());
        req->OnComplete(utils::Status(Code::NOT_FOUND, "Not Found"), {}, "");
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponseMultipleServiceConfig);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r1",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig2);
      }));

  int sequence = 0;

  std::shared_ptr<ConfigManager> config_manager(new ConfigManager(
      global_context_,
      [this, &sequence](const utils::Status& status,
                        const std::vector<std::pair<std::string, int>>& list) {
        sequence++;
      }));

  config_manager->Init();
  ASSERT_EQ(0, sequence);
  raw_env_->RunTimer();
  // One of ServiceConfig download was failed. The callback should not be
  // invoked
  ASSERT_EQ(0, sequence);
  // Succeeded on the next timer event. Invoke the callback function
  raw_env_->RunTimer();
  ASSERT_EQ(1, sequence);
}

TEST_F(ConfigManagerServiceNameConfigIdTest, RolloutSingleServiceConfigUpdate) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponse1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponse2);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r1",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig2);
      }));

  int sequence = 0;

  std::shared_ptr<ConfigManager> config_manager(new ConfigManager(
      global_context_,
      [this, &sequence](const utils::Status& status,
                        const std::vector<std::pair<std::string, int>>& list) {

        ASSERT_EQ(1, list.size());

        // depends on sequence, different service_config will downloaded
        ASSERT_EQ(sequence == 0 ? kServiceConfig1 : kServiceConfig2,
                  list[0].first);

        ASSERT_EQ(100, list[0].second);

        sequence++;
      }));

  config_manager->Init();
  // run first periodic timer
  ASSERT_EQ(0, sequence);
  raw_env_->RunTimer();
  // run second periodic timer
  ASSERT_EQ(1, sequence);
  raw_env_->RunTimer();
  ASSERT_EQ(2, sequence);
}

TEST_F(ConfigManagerServiceNameConfigIdTest,
       RolloutSingleServiceConfigNoupdate) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponse1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/2017-05-01r0",
            req->url());
        req->OnComplete(Status::OK, {}, kServiceConfig1);
      }))
      .WillOnce(Invoke([this](HTTPRequest* req) {
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/rollouts?filter=status=SUCCESS",
            req->url());
        req->OnComplete(Status::OK, {}, kRolloutsResponse1);
      }));

  int sequence = 0;
  std::shared_ptr<ConfigManager> config_manager(new ConfigManager(
      global_context_,
      [this, &sequence](const utils::Status& status,
                        const std::vector<std::pair<std::string, int>>& list) {

        ASSERT_EQ(1, list.size());
        ASSERT_EQ(kServiceConfig1, list[0].first);
        ASSERT_EQ(100, list[0].second);

        sequence++;
      }));

  config_manager->Init();
  // run first periodic timer
  ASSERT_EQ(0, sequence);
  raw_env_->RunTimer();
  // run second periodic timer
  ASSERT_EQ(1, sequence);
  raw_env_->RunTimer();
  // Same rollout_id, no update
  ASSERT_EQ(1, sequence);
}

}  // namespace
}  // namespace api_manager
}  // namespace google
