/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#ifndef ISTIO_MIXERCLIENT_GLOBAL_DICTIONARY_H
#define ISTIO_MIXERCLIENT_GLOBAL_DICTIONARY_H

#include <string>
#include <vector>

namespace istio {
namespace mixerclient {

// Get automatically generated global words.
const std::vector<std::string>& GetGlobalWords();

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_GLOBAL_DICTIONARY_H
