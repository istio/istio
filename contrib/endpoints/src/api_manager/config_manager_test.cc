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

const char kServiceConfig[] = R"(
{
  "name": "endpoints-test.cloudendpointsapis.com",
  "control": {
     "environment": "http://127.0.0.1:808"
  }
})";

const char kServerConfigWithoutServiceNameConfigID[] = R"(
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
  }
}
)";

const char kServerConfigWithoutServiceNameConfigIDMetadataDisabled[] = R"(
{
  "google_authentication_secret": "{}",
  "service_control_config": {
    "report_aggregator_config": {
      "cache_entries": 10000,
      "flush_interval_ms": 1000001232
    },
    "quota_aggregator_config": {
      "cache_entries": 300000,
      "refresh_interval_ms": 1000
    }
  }
}
)";

const char kServerConfigWithUserDefinedMetatdataServer[] = R"(
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
  "service_management_config": {
    "url": "http://servicemanagement.user.com",
    "refresh_interval_ms": 1000
  }
}
)";

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
  "service_name": "service_name_from_server_config"
}
)";

const char kServerConfigWithServiceNameConfigId[] = R"(
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
  "config_id": "2017-05-01r1"
}
)";

const char kGceMetadataWithoutServiceNameConfigId[] = R"(
{
  "project": {
    "projectId": "test-project"
  },
  "instance": {
    "attributes":{
    }
  }
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

const char kGceMetadataWithServiceNameAndNoConfigId[] = R"(
{
  "project": {
    "projectId": "test-project"
  },
  "instance": {
    "attributes":{
      "endpoints-service-name": "service_name_from_metadata"
    }
  }
}
)";

const char kGceMetadataWithServiceNameAndInvalidConfigId[] = R"(
{
  "project": {
    "projectId": "test-project"
  },
  "instance": {
    "attributes":{
      "endpoints-service-name": "service_name_from_metadata",
      "endpoints-service-config-id":"invalid"
    }
  }
}
)";

const char kMetaData[] = R"(
{
    "instance": {
        "attributes": {
            "endpoints-service-config-id": "2017-05-01r0",
            "endpoints-service-name": "service_name_from_meta_data"
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

const char kRolloutsResponseMultipleConfigs[] = R"(
{
  "rollouts": [
    {
      "rolloutId": "2017-05-01r0",
      "createTime": "2017-05-01T22:40:09.884Z",
      "createdBy": "test_user@google.com",
      "status": "SUCCESS",
      "trafficPercentStrategy": {
        "percentages": {
          "2017-05-01r0": 50,
          "2017-05-01r1": 30,
          "2017-05-01r2": 20
        }
      },
      "serviceName": "service_name_from_server_config"
    }
  ]
}
)";

const char kRolloutsResponseFailedRollouts[] = R"(
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

const char kRolloutsResponseFailedRolloutsNextPage[] = R"(
{
  "rollouts": [
    {
      "rolloutId": "2017-05-01r0",
      "createTime": "2017-05-01T22:40:09.884Z",
      "createdBy": "test_user@google.com",
      "status": "FAILED",
      "trafficPercentStrategy": {
        "percentages": {
          "2017-05-01r0": 100
        }
      },
      "serviceName": "service_name_from_server_config"
    }
  ],
  "next_page_token": "next_page_token"
}
)";

const char kRolloutsResponseFailedRolloutsPage2[] = R"(
{
  "rollouts": [
    {
      "rolloutId": "2017-05-01r1",
      "createTime": "2017-05-01T22:40:09.884Z",
      "createdBy": "test_user@google.com",
      "status": "SUCCESS",
      "trafficPercentStrategy": {
        "percentages": {
          "2017-05-01r1": 80,
          "2017-05-01r2": 20
        }
      },
      "serviceName": "service_name_from_server_config"
    }
  ]
}
)";

const char kRolloutsResponseEmpty[] = R"(
{
  "rollouts": []
}
)";

// Both service_name, config_id in server config
class ConfigManagerServiceNameConfigIdTest : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>());
    // save the raw pointer of env before calling std::move(env).
    raw_env_ = env_.get();

    global_context_ = std::make_shared<context::GlobalContext>(
        std::move(env_), kServerConfigWithServiceNameConfigId);

    history_.clear();
  }

  std::unique_ptr<MockApiManagerEnvironment> env_;
  MockApiManagerEnvironment* raw_env_;
  std::shared_ptr<context::GlobalContext> global_context_;
  std::vector<std::string> history_;
};

TEST_F(ConfigManagerServiceNameConfigIdTest,
       TestNoMetadataFetchAndFetchServiceConfigSucceeded) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/"
             "?recursive=true",
             kGceMetadataWithServiceNameAndConfigId},
            {"https://servicemanagement.googleapis.com/v1/services/"
             "service_name_from_server_config/configs/2017-05-01r1",
             kServiceConfig1}};

        history_.push_back(req->url());
        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("service_name_from_server_config", global_context_->service_name());
  ASSERT_EQ("2017-05-01r1", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {

        ASSERT_EQ(1, list.size());
        ASSERT_EQ(kServiceConfig1, list[0].first);
        ASSERT_EQ(100, list[0].second);

        ASSERT_EQ(1, history_.size());
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_server_config/configs/2017-05-01r1",
            history_[0]);
      });
}

TEST_F(ConfigManagerServiceNameConfigIdTest,
       TestNoMetadataFetchAndFailedToFetchServiceConfig) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/"
             "?recursive=true",
             kGceMetadataWithServiceNameAndConfigId}};

        history_.push_back(req->url());
        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("service_name_from_server_config", global_context_->service_name());
  ASSERT_EQ("2017-05-01r1", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {
        ASSERT_EQ("ABORTED: Failed to download the service config",
                  status.ToString());

        ASSERT_EQ(1, history_.size());
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_server_config/configs/2017-05-01r1",
            history_[0]);
      });
}

// service_name in server config, but no config_id
class ConfigManagerServiceNameTest : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>());
    // save the raw pointer of env before calling std::move(env).
    raw_env_ = env_.get();

    std::unique_ptr<Config> config = Config::Create(raw_env_, kServiceConfig);
    ASSERT_NE(config.get(), nullptr);

    global_context_ = std::make_shared<context::GlobalContext>(
        std::move(env_), kServerConfigWithServiceName);
    history_.clear();
  }

  std::unique_ptr<MockApiManagerEnvironment> env_;
  MockApiManagerEnvironment* raw_env_;

  std::shared_ptr<context::GlobalContext> global_context_;
  std::vector<std::string> history_;
};

TEST_F(ConfigManagerServiceNameTest,
       TestServiceNameFromServerConfigConfigIdFromMetadata) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/?recursive=true",
             kGceMetadataWithServiceNameAndConfigId},
            {"https://servicemanagement.googleapis.com/v1/services/"
             "service_name_from_server_config/configs/2017-05-01r1",
             kServiceConfig2}};

        history_.push_back(req->url());
        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("service_name_from_server_config", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {

        ASSERT_EQ("OK", status.ToString());
        ASSERT_EQ("2017-05-01r1", global_context_->config_id());

        ASSERT_EQ(1, list.size());
        ASSERT_EQ(kServiceConfig2, list[0].first);
        ASSERT_EQ(100, list[0].second);

        ASSERT_EQ(2, history_.size());
        ASSERT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  history_[0]);
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_server_config/configs/2017-05-01r1",
            history_[1]);
      });
}

TEST_F(ConfigManagerServiceNameTest,
       TestConfigIdNotDefinedInBothServerConfigAndMetadata) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/?recursive=true",
             kGceMetadataWithServiceNameAndNoConfigId}};

        history_.push_back(req->url());
        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("service_name_from_server_config", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {
        ASSERT_EQ("ABORTED: API config_id is not specified", status.ToString());

        ASSERT_EQ(0, list.size());

        ASSERT_EQ(1, history_.size());
        ASSERT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  history_[0]);
      });
}

// no service_name and config_id in server config
class ConfigManagerNoServiceNameNoConfigIdTest : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>());
    // save the raw pointer of env before calling std::move(env).
    raw_env_ = env_.get();

    global_context_ = std::make_shared<context::GlobalContext>(
        std::move(env_), kServerConfigWithoutServiceNameConfigID);
    history_.clear();
  }

  std::unique_ptr<MockApiManagerEnvironment> env_;
  MockApiManagerEnvironment* raw_env_;

  std::shared_ptr<context::GlobalContext> global_context_;
  std::vector<std::string> history_;
};

TEST_F(ConfigManagerNoServiceNameNoConfigIdTest,
       TestServiceNameFromMetadataConfigIdFromMetadata) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/?recursive=true", kMetaData},
            {"https://servicemanagement.googleapis.com/v1/services/"
             "service_name_from_meta_data/configs/2017-05-01r0",
             kServiceConfig1}};

        history_.push_back(req->url());
        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {
        ASSERT_EQ("OK", status.ToString());
        ASSERT_EQ(1, list.size());
        ASSERT_EQ(kServiceConfig1, list[0].first);
        ASSERT_EQ(100, list[0].second);

        ASSERT_EQ(2, history_.size());
        ASSERT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  history_[0]);
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_meta_data/configs/2017-05-01r0",
            history_[1]);
      });
}

TEST_F(ConfigManagerNoServiceNameNoConfigIdTest,
       TestServiceNameFromMetadataOnly) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/?recursive=true",
             kGceMetadataWithServiceNameAndNoConfigId}};

        history_.push_back(req->url());
        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {
        ASSERT_EQ("ABORTED: API config_id is not specified", status.ToString());

        ASSERT_EQ(1, history_.size());
        ASSERT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  history_[0]);
      });
}

TEST_F(ConfigManagerNoServiceNameNoConfigIdTest,
       TestNoServiceNameAndConfigIdFromGceMetadata) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillOnce(Invoke([](HTTPRequest* req) {

        ASSERT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  req->url());

        req->OnComplete(Status::OK, {}, kGceMetadataWithoutServiceNameConfigId);
      }));

  ASSERT_EQ("", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init([](
      const utils::Status& status,
      const std::vector<std::pair<std::string, int>>& list) {
    ASSERT_EQ("ABORTED: API service name is not specified", status.ToString());
  });
}

TEST_F(ConfigManagerNoServiceNameNoConfigIdTest,
       TestServiceNameAndInvalidConfigIdFromGceMetadata) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        history_.push_back(req->url());

        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/?recursive=true",
             kGceMetadataWithServiceNameAndInvalidConfigId}};

        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {
        ASSERT_EQ("ABORTED: Failed to download the service config",
                  status.ToString());

        ASSERT_EQ(2, history_.size());
        ASSERT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  history_[0]);
        ASSERT_EQ(
            "https://servicemanagement.googleapis.com/v1/services/"
            "service_name_from_metadata/configs/invalid",
            history_[1]);
      });
}

// no service_name and config_id in server config
class ConfigManagerNoServiceNameNoConfigIdMetadataDisabledTest
    : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>());
    // save the raw pointer of env before calling std::move(env).
    raw_env_ = env_.get();

    global_context_ = std::make_shared<context::GlobalContext>(
        std::move(env_),
        kServerConfigWithoutServiceNameConfigIDMetadataDisabled);
    history_.clear();
  }

  std::unique_ptr<MockApiManagerEnvironment> env_;
  MockApiManagerEnvironment* raw_env_;

  std::shared_ptr<context::GlobalContext> global_context_;
  std::vector<std::string> history_;
};

TEST_F(ConfigManagerNoServiceNameNoConfigIdMetadataDisabledTest,
       TestFailedToGetServiceNameAndConfigId) {
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        history_.push_back(req->url());

        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/?recursive=true",
             kGceMetadataWithServiceNameAndInvalidConfigId}};

        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init([this](
      const utils::Status& status,
      const std::vector<std::pair<std::string, int>>& list) {
    ASSERT_EQ("ABORTED: API service name is not specified", status.ToString());

    ASSERT_EQ(0, history_.size());
  });
}

// service_name, config_id were defined in the user defined service management
// service url
class ConfigManagerNoServiceNameNoConfigIdUserDefinedMetadataTest
    : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironment>());
    // save the raw pointer of env before calling std::move(env).
    raw_env_ = env_.get();

    global_context_ = std::make_shared<context::GlobalContext>(
        std::move(env_), kServerConfigWithUserDefinedMetatdataServer);
    history_.clear();
  }

  std::unique_ptr<MockApiManagerEnvironment> env_;
  MockApiManagerEnvironment* raw_env_;
  std::shared_ptr<context::GlobalContext> global_context_;
  std::vector<std::string> history_;
};

TEST_F(ConfigManagerNoServiceNameNoConfigIdUserDefinedMetadataTest,
       TestServiceNameAndConfigIdFromGceMetadata) {
  // FetchGceMetadata responses with headers and status OK.
  EXPECT_CALL(*raw_env_, DoRunHTTPRequest(_))
      .WillRepeatedly(Invoke([this](HTTPRequest* req) {
        std::map<std::string, std::string> data = {
            {"http://localhost/computeMetadata/v1/?recursive=true", kMetaData},
            {"http://servicemanagement.user.com/v1/services/"
             "service_name_from_meta_data/configs/2017-05-01r0",
             kServiceConfig1}};
        history_.push_back(req->url());
        if (data.find(req->url()) == data.end()) {
          req->OnComplete(Status(Code::NOT_FOUND, "Not Found"), {},
                          std::move(data[req->url()]));
        } else {
          req->OnComplete(Status::OK, {}, std::move(data[req->url()]));
        }
      }));

  ASSERT_EQ("", global_context_->service_name());
  ASSERT_EQ("", global_context_->config_id());

  std::shared_ptr<ConfigManager> config_manager(
      new ConfigManager(global_context_));

  config_manager->Init(
      [this](const utils::Status& status,
             const std::vector<std::pair<std::string, int>>& list) {
        ASSERT_EQ("OK", status.ToString());
        ASSERT_EQ(1, list.size());
        ASSERT_EQ(kServiceConfig1, list[0].first);
        ASSERT_EQ(100, list[0].second);

        ASSERT_EQ(2, history_.size());
        ASSERT_EQ("http://localhost/computeMetadata/v1/?recursive=true",
                  history_[0]);
        ASSERT_EQ(
            "http://servicemanagement.user.com/v1/services/"
            "service_name_from_meta_data/configs/2017-05-01r0",
            history_[1]);
      });
}

}  // namespace
}  // namespace service_control_client
}  // namespace google
