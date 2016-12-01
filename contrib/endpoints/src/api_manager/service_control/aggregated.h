/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
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
