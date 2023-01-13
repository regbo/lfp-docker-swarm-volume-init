#!/bin/sh
set -e

if [ -z $CONTAINER_ID ]; then
  OVERLAY_ID=`cat /proc/self/mountinfo | grep -i overlay | sed -n "s/.\+upperdir\\=\\(.\+\\)\\/diff.\+/\1/p"`
  export CONTAINER_ID=`docker inspect -f $'{{.ID}}\t{{.Name}}\t{{.GraphDriver.Data.MergedDir}}' $(docker ps -aq) | grep $OVERLAY_ID | sed -n "s/\t\+.\+//p"`
fi

/app