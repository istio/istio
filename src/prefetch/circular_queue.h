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

#ifndef ISTIO_PREFETCH_CIRCULAR_QUEUE_H_
#define ISTIO_PREFETCH_CIRCULAR_QUEUE_H_

#include <functional>
#include <vector>

namespace istio {
namespace prefetch {

// Define a circular FIFO queue
// Supported classes should support copy operator.
template <class T>
class CircularQueue {
 public:
  explicit CircularQueue(int size);

  // Push an item to the tail
  void Push(const T& v);

  // Pop up an item from the head
  void Pop();

  // Allow modifying the head item.
  T* Head();

  // Calls the fn function for each element from head to tail.
  void Iterate(std::function<bool(T&)> fn);

 private:
  std::vector<T> nodes_;
  int head_;
  int tail_;
  int count_;
};

template <class T>
CircularQueue<T>::CircularQueue(int size)
    : nodes_(size), head_(0), tail_(0), count_(0) {}

template <class T>
void CircularQueue<T>::Push(const T& v) {
  if (head_ == tail_ && count_ > 0) {
    size_t size = nodes_.size();
    nodes_.resize(size * 2);
    for (int i = 0; i <= head_; i++) {
      // Use the copy operator of class T
      nodes_[size + i] = nodes_[i];
    }
    tail_ += size;
  }
  nodes_[tail_] = v;
  tail_ = (tail_ + 1) % nodes_.size();
  count_++;
}

template <class T>
void CircularQueue<T>::Pop() {
  if (count_ == 0) return;
  head_ = (head_ + 1) % nodes_.size();
  count_--;
}

template <class T>
T* CircularQueue<T>::Head() {
  if (count_ == 0) return nullptr;
  return &nodes_[head_];
}

template <class T>
void CircularQueue<T>::Iterate(std::function<bool(T&)> fn) {
  if (count_ == 0) return;
  int i = head_;
  while (i != tail_) {
    if (!fn(nodes_[i])) return;
    i = (i + 1) % nodes_.size();
  }
}

}  // namespace prefetch
}  // namespace istio

#endif  // ISTIO_PREFETCH_CIRCULAR_QUEUE_H_
