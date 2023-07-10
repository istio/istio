#!/bin/bash

source prow/tracing-auto.sh

tracing::init

function nested() {
  sleep .1
  sleep .2
  deep
}
tracing::decorate nested

function deep() {
  tracing::run "in-deep" sleep .1
}
tracing::decorate deep

nested
