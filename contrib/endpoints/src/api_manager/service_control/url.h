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
#ifndef API_MANAGER_SERVICE_CONTROL_URL_H_
#define API_MANAGER_SERVICE_CONTROL_URL_H_

#include "google/api/service.pb.h"
#include "src/api_manager/proto/server_config.pb.h"

namespace google {
namespace api_manager {
namespace service_control {

// This class implements service control url related logic.
class Url {
 public:
  Url(const ::google::api::Service* service,
      const ::google::api_manager::proto::ServerConfig* server_config);

  // Pre-computed url for service control.
  const std::string& service_control() const { return service_control_; }
  const std::string& check_url() const { return check_url_; }
  const std::string& report_url() const { return report_url_; }

 private:
  // Pre-computed url for service control methods.
  std::string service_control_;
  std::string check_url_;
  std::string report_url_;
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_URL_H_
