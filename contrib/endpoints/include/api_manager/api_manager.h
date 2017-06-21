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
#ifndef API_MANAGER_API_MANAGER_H_
#define API_MANAGER_API_MANAGER_H_

// An API Manager interface.

#include <memory>
#include <string>

#include "contrib/endpoints/include/api_manager/env_interface.h"
#include "contrib/endpoints/include/api_manager/request.h"
#include "contrib/endpoints/include/api_manager/request_handler_interface.h"
#include "contrib/endpoints/include/api_manager/service_control.h"
#include "google/api/service.pb.h"

namespace google {
namespace api_manager {

// Data to summarize the API Manager statistics.
// Important note: please don't use std::string. These fields are directly
// copied into a shared memory.
struct ApiManagerStatistics {
  service_control::Statistics service_control_statistics;
};

class ApiManager {
 public:
  virtual ~ApiManager() {}

  // Returns true if either auth is required or service control is configured.
  virtual bool Enabled() const = 0;

  // Gets the service name.
  virtual const std::string &service_name() const = 0;

  // Gets the service config by config_id.
  virtual const ::google::api::Service &service(
      const std::string &config_id) const = 0;

  // Initializes the API Manager. It should be called:
  // 1) Before first CreateRequestHandler().
  // 2) After certain provided environment is ready. Specifically for Nginx,
  // It should be called inside InitProcess() of each worker process.
  virtual utils::Status Init() = 0;

  // Closes the API Manager. After this, CreateRequestHandler() should not be
  // called.
  virtual utils::Status Close() = 0;

  // server_config has an option to disable logging to nginx error.log.
  // This function checks server_config for that flag.
  virtual bool get_logging_status_disabled() = 0;

  // Creates a RequestHandler to handle check and report for each request.
  // Its usage:
  //  1) Creates a RequestHandler object for each request,
  //
  //    request_handler = api_manager->CreateRequestHandler(request);
  //
  //  2) Before forwarding the request to backend, calls Check().
  //
  //    request_handler->Check([](utils::Status status) {
  //               check status;
  //          });
  //
  //  3) After the request is finished, calls Report().
  //
  //    request_handler->Report(response, [](){});
  //
  virtual std::unique_ptr<RequestHandlerInterface> CreateRequestHandler(
      std::unique_ptr<Request> request) = 0;

  // To get the api manager statistics.
  virtual utils::Status GetStatistics(
      ApiManagerStatistics *statistics) const = 0;

  // Load service rollouts. This can be called only once, the data is from
  // server_config.
  virtual utils::Status LoadServiceRollouts() = 0;

 protected:
  ApiManager() {}

 private:
  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(ApiManager);
};

class ApiManagerFactory {
 public:
  ApiManagerFactory() {}
  virtual ~ApiManagerFactory() {}

  // Create an ApiManager object.
  std::shared_ptr<ApiManager> CreateApiManager(
      std::unique_ptr<ApiManagerEnvInterface> env,
      const std::string &server_config);
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_API_MANAGER_H_
