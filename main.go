package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	//"strconv"
	"strings"
)

type WheaterRecord struct {
	Time          string  `json:"time"`
	Model         string  `json:"model"`
	Id            int     `json:"id"`
	Channel       int     `json:"channel"`
	BatteryOk    int     `json:"battery_ok"`
	TemperatureC float64 `json:"temperature_C"`
	Humidity      int     `json:"humidity"`
	WindAvgMs  float64 `json:"wind_avg_m_s"`
	WindDirDeg  float64 `json:"wind_dir_deg"`
	RadioClock   string  `json:"radio_clock"`
	Mic           string  `json:"mic"`
}

var records []WheaterRecord

func readVals(filename string) []WheaterRecord {

	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	var buffer bytes.Buffer
	for scanner.Scan() {
		buffer.WriteString(scanner.Text())
		buffer.WriteString("\n")
	}

	lines := strings.Split(buffer.String(), "\n")

	for _, line := range lines {

			var r WheaterRecord

			err := json.Unmarshal([]byte(line), &r)
			if err != nil {
				fmt.Printf("Error decoding JSON: %v\n", err)
				continue
			}

			records = append(records, r)
		}

	return records

}


func msToKmh(input float64) float64 {
 	return input * 1 / 0.27777777777778
}

func main() {

	var live bool
	var input string
	flag.BoolVar(&live, "l", false, "Live mode.")
	flag.StringVar(&input, "i", "graph.txt", "Input logfile.")

	flag.Parse()

	if live {

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
			r.WindAvgMs = msToKmh(r.WindAvgMs)

			fmt.Printf("time: %s, temp: %f, hum: %d, wind_avg_KmH: %f, wind_dir_deg: %f\n", r.Time, r.TemperatureC, r.Humidity, r.WindAvgMs, r.WindDirDeg)

			records = append(records, r)
			//fmt.Printf("got %04d records\n", len(records))
		}
	}


	if input != "" {
		records = readVals(input)

		for i, record := range records {
			//convert m/s to km/h
			records[i].WindAvgMs = msToKmh(records[i].WindAvgMs)
			fmt.Printf("time: %s, temp: %f, hum: %d, wind_avg_KmH: %f, wind_dir_deg: %f\n", record.Time, record.TemperatureC, record.Humidity, record.WindAvgMs, record.WindDirDeg)

		}
	}

}
