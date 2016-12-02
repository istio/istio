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

#include "src/api_manager/utils/url_util.h"

namespace google {
namespace api_manager {
namespace utils {

std::string GetUrlContent(const std::string &url) {
  static const std::string https_prefix = "https://";
  static const std::string http_prefix = "http://";
  std::string result;
  if (url.compare(0, https_prefix.size(), https_prefix) == 0) {
    result = url.substr(https_prefix.size());
  } else if (url.compare(0, http_prefix.size(), http_prefix) == 0) {
    result = url.substr(http_prefix.size());
  } else {
    result = url;
  }
  if (result.back() == '/') {
    result.pop_back();
  }
  return result;
}

}  // namespace utils
}  // namespace api_manager
}  // namespace google
