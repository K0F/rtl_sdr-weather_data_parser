# rtl_sdr-weather_data_parser
rtl_433 to golang

## parsing rtl_433 from EMOS E6016

```
go mod tidy
go build
rtl_433 -R 214 -g 20 -F json | ./weather_graph 
```

