#!/bin/bash
cat graph.json >> backup.json
rtl_433 -R 214 -g 20 -F json > graph.json &
./wheater_graph
