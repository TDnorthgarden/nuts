#!/bin/bash

POD_ID=$(crictl runp pod-config.json)

CONTAINER_ID=$(crictl create $POD_ID container-config.json pod-config.json)

crictl start $CONTAINER_ID

crictl stop $CONTAINER_ID

crictl rm $CONTAINER_ID

crictl stopp $POD_ID

crictl rmp $POD_ID

