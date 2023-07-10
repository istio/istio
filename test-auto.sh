#!/bin/bash

source prow/tracing-auto.sh

tracing::auto::init

function nested() {
   tracing::run 'sleepy' sleep .1
   deep
   sleep .1
   deep
}

function deep() {
   deep-deep
   sleep .05
   deep-deep
}

function deep-deep() {
   sleep .1
}

nested
./test.sh
./test-decorate.sh
