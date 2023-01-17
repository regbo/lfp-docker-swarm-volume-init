#!/bin/sh
set -e

if [ -z $CONTAINER_ID ]; then
  export CONTAINER_ID=$(docker inspect --format="{{.Id}}" $(hostname))
fi

/app