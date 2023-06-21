package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"

	//"strconv"
	"net/http"
	"strings"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

type WheaterRecord struct {
	Time         string  `json:"time"`
	Model        string  `json:"model"`
	Id           int     `json:"id"`
	Channel      int     `json:"channel"`
	BatteryOk    int     `json:"battery_ok"`
	TemperatureC float64 `json:"temperature_C"`
	Humidity     int     `json:"humidity"`
	WindAvgMs    float64 `json:"wind_avg_m_s"`
	WindDirDeg   float64 `json:"wind_dir_deg"`
	RadioClock   string  `json:"radio_clock"`
	Mic          string  `json:"mic"`
}

var records []WheaterRecord
var INPUT_FILE string
var PORT = 8080

func reloadData(input string) []WheaterRecord {
	records = make([]WheaterRecord, 0)

	records = readVals(input)

	for i, _ := range records {
		//convert m/s to km/h
		records[i].WindAvgMs = msToKmh(records[i].WindAvgMs)
		//fmt.Printf("time: %s, temp: %f, hum: %d, wind_avg_KmH: %f, wind_dir_deg: %f\n", record.Time, record.TemperatureC, record.Humidity, record.WindAvgMs, record.WindDirDeg)

	}
	return records
}

// generate random data for line chart
func getTemperature(records []WheaterRecord) []opts.LineData {
	items := make([]opts.LineData, 0)
	for _, record := range records {
		items = append(items, opts.LineData{Value: record.TemperatureC})
	}
	return items
}

// generate random data for line chart
func getHumidity(records []WheaterRecord) []opts.LineData {
	items := make([]opts.LineData, 0)
	for _, record := range records {
		items = append(items, opts.LineData{Value: record.Humidity})
	}
	return items
}

// generate random data for line chart
func getWind(records []WheaterRecord) []opts.LineData {
	items := make([]opts.LineData, 0)
	for _, record := range records {
		items = append(items, opts.LineData{Value: record.WindAvgMs})
	}
	return items
}

func readVals(filename string) []WheaterRecord {

	file, err := os.Open(filename)
	if err != nil {
    fmt.Println("Got error reading file: %s",err.Error())
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
      fmt.Printf("Error decoding JSON: %v len: %v\n", err.Error(), len(records))
			continue
		}

		records = append(records, r)
	}

	for next := true; next; next = len(records) > 1500 {
		records = records[1:len(records)]
	}

	return records

}

func msToKmh(input float64) float64 {
	return input * 1 / 0.27777777777778
}

func main() {

	var port int
	var input string
	//flag.BoolVar(&live, "l", false, "Live mode.")
	flag.StringVar(&input, "i", "graph.json", "Input logfile.")
	flag.IntVar(&port, "p", 8080, "Port to listen.")

	flag.Parse()

	/*
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
		}*/

	if input != "" {
		INPUT_FILE = input
	}

	if port != 8080 {
		PORT = port
	}

	http.HandleFunc("/", httpserver)
    http.HandleFunc("/weather.json", weatherHandler)
	log.Printf("Serving @ port: %d\n", PORT)
	http.ListenAndServe(fmt.Sprintf(":%d", PORT), nil)
}


func weatherHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getWeather(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}


func getWeather(w http.ResponseWriter, r *http.Request) {
	// Assume you have a slice of WheaterRecord data
	weatherData := readVals(INPUT_FILE)
    
    fmt.Printf("Got %v records\n",len(records))

	// Marshal the weather data into JSON
	jsonData, err := json.Marshal(weatherData)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Failed to marshal JSON data: %v", err)
		return
	}

	// Set the response content type and write the JSON data
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonData)
}

// generate random data for line chart
func generateLineItems() []opts.LineData {
	items := make([]opts.LineData, 0)
	for i := 0; i < 7; i++ {
		items = append(items, opts.LineData{Value: rand.Intn(300)})
	}
	return items
}

func httpserver(w http.ResponseWriter, request *http.Request) {

	reloadData(INPUT_FILE)

	// create a new line instance
	line := charts.NewLine()
	// set some global options like Title/Legend/ToolTip or anything else
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWesteros}),
		charts.WithTitleOpts(opts.Title{
			Title:    "Meteo data SOB23",
			Subtitle: "Data from local station EMOS E6016",
		}))
    //.SetLogo("shot.jpg")))

	// Extract the 'Name' field as a slice of strings
	times := make([]string, len(records))
	for i, record := range records {
		times[i] = record.Time
	}

	// Put data into instance

	line.SetXAxis(times).
		AddSeries("Temperature ˚C", getTemperature(records)).
		AddSeries("Humidity rel. %", getHumidity(records)).
		AddSeries("Wind speed km/h", getWind(records)).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: true}))
	line.Render(w)
}
