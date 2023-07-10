#!/bin/bash

source prow/tracing-manual.sh

tracing::init

function nested() {
  tracing::run "child1" sleep .1
  tracing::run "child2" sleep .2
  tracing::run "deep" deep
}

function deep() {
  tracing::run "in-deep" sleep .1
}

tracing::run "parent" nested
