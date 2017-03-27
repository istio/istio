#!/usr/bin/env bash
# Start Redis for redis quota adapter
# TODO: figure out where should be the redis path

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

cd $ROOT/redis-stable
echo "start redis..."
redis-server
