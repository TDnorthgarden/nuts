#!/bin/bash

POD_ID=$(crictl runp pod-config.json)

crictl stopp $POD_ID

crictl rmp $POD_ID

