/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_SERVICE_CONTROL_AGGREGATED_H_
#define API_MANAGER_SERVICE_CONTROL_AGGREGATED_H_

#include "google/api/service.pb.h"
#include "google/api/servicecontrol/v1/service_controller.pb.h"
#include "include/api_manager/env_interface.h"
#include "src/api_manager/auth/service_account_token.h"
#include "src/api_manager/cloud_trace/cloud_trace.h"
#include "src/api_manager/proto/server_config.pb.h"
#include "src/api_manager/service_control/interface.h"
#include "src/api_manager/service_control/proto.h"
#include "src/api_manager/service_control/url.h"
#include "third_party/service-control-client-cxx/include/service_control_client.h"

#include <list>
#include <mutex>

namespace google {
namespace api_manager {
namespace service_control {

// This implementation uses service-control-client-cxx module.
class Aggregated : public Interface {
 public:
  static Interface* Create(const ::google::api::Service& service,
                           const proto::ServerConfig* server_config,
                           ApiManagerEnvInterface* env,
                           auth::ServiceAccountToken* sa_token);

  virtual ~Aggregated();

  virtual utils::Status Report(const ReportRequestInfo& info);

  virtual void Check(
      const CheckRequestInfo& info, cloud_trace::CloudTraceSpan* parent_span,
      std::function<void(utils::Status, const CheckResponseInfo&)> on_done);

  virtual utils::Status Init();
  virtual utils::Status Close();

  virtual utils::Status GetStatistics(Statistics* stat) const;

 private:
  // A timer object to wrap PeriodicTimer
  class ApiManagerPeriodicTimer
      : public ::google::service_control_client::PeriodicTimer {
   public:
    ApiManagerPeriodicTimer(
        std::unique_ptr<::google::api_manager::PeriodicTimer> esp_timer)
        : esp_timer_(std::move(esp_timer)) {}

    // Cancels the timer.
    virtual void Stop() {
      if (esp_timer_) esp_timer_->Stop();
    }

   private:
    std::unique_ptr<::google::api_manager::PeriodicTimer> esp_timer_;
  };

  // The protobuf pool to reuse protobuf. Performance tests showed that reusing
  // protobuf is faster than allocating a new protobuf.
  template <class Type>
  class ProtoPool {
   public:
    // Allocates a protobuf. If there is one in the pool, uses it, otherwise
    // creates a new one.
    std::unique_ptr<Type> Alloc();
    // Frees a protobuf. If pool did not reach maximum size, stores it in the
    // pool, otherwise frees it.
    void Free(std::unique_ptr<Type> item);

   private:
    // Protobuf pool to store used protobufs.
    std::list<std::unique_ptr<Type>> pool_;
    // Mutex to protect the protobuf pool.
    std::mutex mutex_;
  };

  friend class AggregatedTestWithMockedClient;
  // Constructor for unit-test only.
  Aggregated(
      const std::set<std::string>& logs, ApiManagerEnvInterface* env,
      std::unique_ptr<::google::service_control_client::ServiceControlClient>
          client);
  // The constructor.
  Aggregated(const ::google::api::Service& service,
             const proto::ServerConfig* server_config,
             ApiManagerEnvInterface* env, auth::ServiceAccountToken* sa_token,
             const std::set<std::string>& logs,
             const std::set<std::string>& metrics,
             const std::set<std::string>& labels);

  // Calls to service control server.
  template <class RequestType, class ResponseType>
  void Call(const RequestType& request, ResponseType* response,
            ::google::service_control_client::TransportDoneFunc on_done,
            cloud_trace::CloudTraceSpan* parent_span);

  // Gets the auth token to access service control server.
  const std::string& GetAuthToken();

  // the sevice config.
  const ::google::api::Service* service_;
  // the server config.
  const proto::ServerConfig* server_config_;

  // The Api Manager environment interface.
  ApiManagerEnvInterface* env_;

  // service account token.
  auth::ServiceAccountToken* sa_token_;

  // The object to fill service control Check and Report protobuf.
  Proto service_control_proto_;

  // Stores service control urls.
  Url url_;

  // The service control client instance.
  std::unique_ptr<::google::service_control_client::ServiceControlClient>
      client_;
  // The protobuf pool to reuse CheckRequest protobuf.
  ProtoPool<::google::api::servicecontrol::v1::CheckRequest> check_pool_;
  // The protobuf pool to reuse ReportRequest protobuf.
  ProtoPool<::google::api::servicecontrol::v1::ReportRequest> report_pool_;

  // Mismatched config ID received for a check request
  std::string mismatched_check_config_id;

  // Mismatched config ID received for a report request
  std::string mismatched_report_config_id;

  // Maximum report size send to server.
  uint64_t max_report_size_;
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_AGGREGATED_H_
