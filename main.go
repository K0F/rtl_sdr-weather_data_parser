package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type WheaterRecord struct {
	Time          string  `json:"time"`
	Model         string  `json:"model"`
	Id            int     `json:"id"`
	Channel       int     `json:"channel"`
	Battery_ok    int     `json:"battery_ok"`
	Temperature_C float64 `json:"temperature_C"`
	Humidity      int     `json:"humidity"`
	Wind_avg_m_s  float64 `json:"wind_avg_m_s"`
	Wind_dir_deg  float64 `json:"wind_dir_deg"`
	Radio_clock   string  `json:"radio_clock"`
	Mic           string  `json:"mic"`
}

var records []WheaterRecord

func main() {

	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		data := scanner.Bytes()
		var r WheaterRecord

		err := json.Unmarshal(data, &r)
		if err != nil {
			fmt.Printf("Error decoding JSON: %v\n", err)
			continue
		}

		//convert m/s to km/h
		r.Wind_avg_m_s = r.Wind_avg_m_s * 1 / 0.27777777777778

		fmt.Printf("time: %s, temp: %f, hum: %d, wind_avg_KmH: %f, wind_dir_deg: %f\n", r.Time, r.Temperature_C, r.Humidity, r.Wind_avg_m_s, r.Wind_dir_deg)

		records = append(records, r)
		//fmt.Printf("got %04d records\n", len(records))
	}

}
