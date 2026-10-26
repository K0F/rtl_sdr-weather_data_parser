#!/bin/sh
SRC=/root/rtl_sdr-weather_data_parser/graph.json
TARGET=/root/rtl_sdr-weather_data_parser/backup.json
cat "$SRC" >> /var/log/weather_data_all.json
mv "$SRC" "$TARGET"
cat "$TARGET" | tail -n 1600 > "$SRC"
