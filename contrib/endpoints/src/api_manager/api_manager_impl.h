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
#ifndef API_MANAGER_API_MANAGER_IMPL_H_
#define API_MANAGER_API_MANAGER_IMPL_H_

#include "include/api_manager/api_manager.h"
#include "src/api_manager/context/service_context.h"
#include "src/api_manager/service_control/interface.h"

namespace google {
namespace api_manager {

class CheckWorkflow;

// Implements ApiManager interface.
class ApiManagerImpl : public ApiManager {
 public:
  ApiManagerImpl(std::unique_ptr<ApiManagerEnvInterface> env,
                 std::unique_ptr<Config> config);

  virtual bool Enabled() const { return service_context_->Enabled(); }

  virtual const std::string &service_name() const {
    return service_context_->service_name();
  }

  virtual const ::google::api::Service &service() const {
    return service_context_->service();
  }

  virtual void SetMetadataServer(const std::string &server) {
    service_context_->SetMetadataServer(server);
  }

  virtual utils::Status SetClientAuthSecret(const std::string &secret) {
    return service_context_->service_account_token()->SetClientAuthSecret(
        secret);
  }

  virtual utils::Status Init() {
    if (service_context_->cloud_trace_aggregator()) {
      service_context_->cloud_trace_aggregator()->Init();
    }
    if (service_control()) {
      return service_control()->Init();
    } else {
      return utils::Status::OK;
    }
  }

  virtual utils::Status Close() {
    if (service_context_->cloud_trace_aggregator()) {
      service_context_->cloud_trace_aggregator()->SendAndClearTraces();
    }
    if (service_control()) {
      return service_control()->Close();
    } else {
      return utils::Status::OK;
    }
  }

  virtual std::unique_ptr<RequestHandlerInterface> CreateRequestHandler(
      std::unique_ptr<Request> request);

  virtual utils::Status GetStatistics(ApiManagerStatistics *statistics) const {
    if (service_control()) {
      return service_control()->GetStatistics(
          &statistics->service_control_statistics);
    } else {
      return utils::Status::OK;
    }
  }

  virtual bool get_logging_status_disabled() {
    return service_context_->DisableLogStatus();
  };

 private:
  service_control::Interface *service_control() const {
    return service_context_->service_control();
  }

  // The check work flow.
  std::shared_ptr<CheckWorkflow> check_workflow_;

  // Service context
  std::shared_ptr<context::ServiceContext> service_context_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_API_MANAGER_IMPL_H_
