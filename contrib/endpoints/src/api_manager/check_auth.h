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
#ifndef API_MANAGER_CHECK_AUTH_H_
#define API_MANAGER_CHECK_AUTH_H_

#include "include/api_manager/utils/status.h"
#include "src/api_manager/context/request_context.h"

namespace google {
namespace api_manager {

// This function checks auth for a given request.
// It is called by CheckWorkflow class when processing a request.
void CheckAuth(std::shared_ptr<context::RequestContext> context,
               std::function<void(utils::Status status)> continuation);

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_CHECK_AUTH_H_
