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
#ifndef API_MANAGER_RESPONSE_H_
#define API_MANAGER_RESPONSE_H_

#include <map>
#include <string>

#include "include/api_manager/protocol.h"
#include "include/api_manager/service_control.h"
#include "include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Response provides an interface for CallHandler::StartReport and
// CallHandler::CompleteReport to use to query information about a
// response.
class Response {
 public:
  virtual ~Response() {}

  // Returns the status associated with the response.
  virtual utils::Status GetResponseStatus() = 0;

  // Returns the size of the initial request, in bytes.
  virtual std::size_t GetRequestSize() = 0;

  // Returns the size of the response, in bytes.
  virtual std::size_t GetResponseSize() = 0;

  // Gets the latency info.
  virtual utils::Status GetLatencyInfo(service_control::LatencyInfo *info) = 0;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_RESPONSE_H_
