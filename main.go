package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
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
	RainMm       float64 `json:"rain_mm"`
	RadioClock   string  `json:"radio_clock"`
	Mic          string  `json:"mic"`
}

type RawRecord struct {
	Time         string   `json:"time"`
	Model        string   `json:"model"`
	Id           int      `json:"id"`
	Channel      int      `json:"channel"`
	BatteryOk    int      `json:"battery_ok"`
	TemperatureC *float64 `json:"temperature_C"`
	Humidity     *int     `json:"humidity"`
	WindAvgMs    *float64 `json:"wind_avg_m_s"`
	WindDirDeg   *float64 `json:"wind_dir_deg"`
	RainMm       *float64 `json:"rain_mm"`
	RadioClock   string   `json:"radio_clock"`
	Mic          string   `json:"mic"`
}

var (
	recordsLock sync.RWMutex
	records     []WheaterRecord
	INPUT_FILE  = "graph.json"
	PORT        = 8080
)

func reloadData(input string) []WheaterRecord {
	records = make([]WheaterRecord, 0)
	records = readVals(input)

	for i := range records {
		// convert m/s to km/h
		records[i].WindAvgMs = msToKmh(records[i].WindAvgMs)
	}
	return records
}

// generate data for line chart
func getTemperature(recs []WheaterRecord) []opts.LineData {
	items := make([]opts.LineData, 0)
	for _, record := range recs {
		items = append(items, opts.LineData{Value: record.TemperatureC})
	}
	return items
}

// generate data for line chart
func getHumidity(recs []WheaterRecord) []opts.LineData {
	items := make([]opts.LineData, 0)
	for _, record := range recs {
		items = append(items, opts.LineData{Value: record.Humidity})
	}
	return items
}

// generate data for line chart
func getWind(recs []WheaterRecord) []opts.LineData {
	items := make([]opts.LineData, 0)
	for _, record := range recs {
		items = append(items, opts.LineData{Value: record.WindAvgMs})
	}
	return items
}

// generate data for line chart
func getRain(recs []WheaterRecord) []opts.LineData {
	items := make([]opts.LineData, 0)
	for _, record := range recs {
		items = append(items, opts.LineData{Value: record.RainMm})
	}
	return items
}

func readVals(filename string) []WheaterRecord {
	recs := make([]WheaterRecord, 0)

	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Got error reading file: %s\n", err.Error())
		return recs
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var buffer bytes.Buffer
	for scanner.Scan() {
		buffer.WriteString(scanner.Text())
		buffer.WriteString("\n")
	}

	lines := strings.Split(buffer.String(), "\n")

	var lastRainMm float64
	var lastTemp float64
	var lastHum int
	var lastWind float64
	var lastWindDir float64
	var hasWeather bool

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		var raw RawRecord
		err := json.Unmarshal([]byte(line), &raw)
		if err != nil {
			continue
		}

		var r WheaterRecord
		r.Time = raw.Time
		r.Model = raw.Model
		r.Id = raw.Id
		r.Channel = raw.Channel
		r.BatteryOk = raw.BatteryOk
		r.RadioClock = raw.RadioClock
		r.Mic = raw.Mic

		// Update accumulated rain if present
		if raw.RainMm != nil {
			lastRainMm = *raw.RainMm
		}
		r.RainMm = lastRainMm

		// Handle weather vs rain packet
		if raw.TemperatureC != nil {
			r.TemperatureC = *raw.TemperatureC
			lastTemp = r.TemperatureC
			hasWeather = true
		} else if hasWeather {
			r.TemperatureC = lastTemp
		}

		if raw.Humidity != nil {
			r.Humidity = *raw.Humidity
			lastHum = r.Humidity
		} else if hasWeather {
			r.Humidity = lastHum
		}

		if raw.WindAvgMs != nil {
			r.WindAvgMs = *raw.WindAvgMs
			lastWind = r.WindAvgMs
		} else if hasWeather {
			r.WindAvgMs = lastWind
		}

		if raw.WindDirDeg != nil {
			r.WindDirDeg = *raw.WindDirDeg
			lastWindDir = r.WindDirDeg
		} else if hasWeather {
			r.WindDirDeg = lastWindDir
		}

		recs = append(recs, r)
	}

	for len(recs) > 1500 {
		recs = recs[1:]
	}

	return recs
}

func msToKmh(input float64) float64 {
	return input * 1 / 0.27777777777778
}

func main() {
	var port int
	var input string
	flag.StringVar(&input, "i", "graph.json", "Input logfile.")
	flag.IntVar(&port, "p", 8080, "Port to listen.")
	flag.Parse()

	if input != "" {
		INPUT_FILE = input
	}
	if port != 8080 {
		PORT = port
	}

	recordsLock.Lock()
	reloadData(INPUT_FILE)
	recordsLock.Unlock()

	r := mux.NewRouter()

	// Add CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.HandleFunc("/", httpserver)
	r.HandleFunc("/weather.json", weatherHandler)
	r.HandleFunc("/last_time", lastTimeHandler)
	r.HandleFunc("/events", eventsHandler)

	c := cors.Default().Handler(r)
	log.Printf("Serving @ port: %d\n", PORT)
	http.ListenAndServe(fmt.Sprintf(":%d", PORT), c)
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
	weatherData := readVals(INPUT_FILE)
	fmt.Printf("Got %v records\n", len(weatherData))

	jsonData, err := json.Marshal(weatherData)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Failed to marshal JSON data: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonData)
}

func lastTimeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	recordsLock.RLock()
	needReload := len(records) == 0
	recordsLock.RUnlock()

	if needReload {
		recordsLock.Lock()
		reloadData(INPUT_FILE)
		recordsLock.Unlock()
	}

	recordsLock.RLock()
	defer recordsLock.RUnlock()
	if len(records) > 0 {
		fmt.Fprintf(w, "%s", records[len(records)-1].Time)
	} else {
		fmt.Fprintf(w, "")
	}
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastModTime := time.Time{}
	if fi, err := os.Stat(INPUT_FILE); err == nil {
		lastModTime = fi.ModTime()
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Initial ping
	fmt.Fprintf(w, ": ping\n\n")
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case <-ticker.C:
			if fi, err := os.Stat(INPUT_FILE); err == nil {
				if fi.ModTime().After(lastModTime) {
					lastModTime = fi.ModTime()
					fmt.Fprintf(w, "data: reload\n\n")
					flusher.Flush()
				}
			}
		}
	}
}

func httpserver(w http.ResponseWriter, request *http.Request) {
	recordsLock.Lock()
	recs := reloadData(INPUT_FILE)
	recordsLock.Unlock()

	line := charts.NewLine()
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:  types.ThemeWesteros,
			Width:  "100%",
			Height: "82vh",
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    "Meteo data SOB23",
			Subtitle: "EMOS E6016 Live Weather Station (Auto-reloads on new data)",
			TitleStyle: &opts.TextStyle{
				Color: "#e2e8f0",
			},
			SubtitleStyle: &opts.TextStyle{
				Color: "#94a3b8",
			},
		}),
		charts.WithColorsOpts(opts.Colors{
			"#ff5722", // Temperature: vibrant warm orange-red
			"#00bcd4", // Humidity: cool cyan
			"#10b981", // Wind: emerald green
			"#7c3aed", // Rainfall: deep violet
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    true,
			Trigger: "axis",
			AxisPointer: &opts.AxisPointer{
				Type: "cross",
			},
		}),
		charts.WithLegendOpts(opts.Legend{
			Show: true,
			Top:  "25px",
			TextStyle: &opts.TextStyle{
				Color: "#cbd5e1",
			},
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:  "slider",
			Start: 60,
			End:   100,
		}),
	)

	times := make([]string, len(recs))
	for i, record := range recs {
		times[i] = record.Time
	}

	line.SetXAxis(times).
		AddSeries("Temperature ˚C", getTemperature(recs), charts.WithLineChartOpts(opts.LineChart{Smooth: true})).
		AddSeries("Humidity rel. %", getHumidity(recs), charts.WithLineChartOpts(opts.LineChart{Smooth: true})).
		AddSeries("Wind speed km/h", getWind(recs), charts.WithLineChartOpts(opts.LineChart{Smooth: true})).
		AddSeries("Rainfall mm", getRain(recs), charts.WithLineChartOpts(opts.LineChart{Smooth: true})).
		SetSeriesOptions(
			charts.WithLineStyleOpts(opts.LineStyle{Width: 2.5}),
		)

	var buf bytes.Buffer
	line.Render(&buf)
	htmlStr := buf.String()

	customHead := `
<style>
    html, body {
        margin: 0 !important;
        padding: 0 !important;
        width: 100% !important;
        height: 100% !important;
        background-color: #0b0f19 !important;
        color: #f1f5f9 !important;
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif !important;
        overflow-x: hidden !important;
    }
    .container {
        margin: 0 !important;
        padding: 10px 15px !important;
        width: 100% !important;
        max-width: 100% !important;
        box-sizing: border-box !important;
        display: block !important;
    }
    .item {
        width: 100% !important;
        margin: 0 !important;
    }
    #live-status {
        position: fixed;
        bottom: 8px;
        right: 15px;
        font-size: 11px;
        color: #64748b;
        background: rgba(15, 23, 42, 0.85);
        padding: 4px 10px;
        border-radius: 4px;
        border: 1px solid #1e293b;
        z-index: 9999;
        pointer-events: none;
    }
</style>
`

	customBottom := `
<div id="live-status">● Live Auto-Reload Active</div>
<script>
(function() {
    window.addEventListener('resize', function() {
        var charts = document.querySelectorAll('.item');
        for (var i = 0; i < charts.length; i++) {
            var inst = echarts.getInstanceByDom(charts[i]);
            if (inst) {
                inst.resize({ width: 'auto', height: window.innerHeight * 0.82 });
            }
        }
    });

    var lastTime = "";
    function reloadPage() {
        var status = document.getElementById('live-status');
        if (status) status.innerText = "⟳ New data arrived, refreshing...";
        setTimeout(function() {
            location.reload();
        }, 500);
    }

    async function checkTime() {
        try {
            var r = await fetch('/last_time?t=' + Date.now());
            if (r.ok) {
                var t = await r.text();
                if (lastTime && t && t !== lastTime) {
                    reloadPage();
                    return;
                }
                if (t) lastTime = t;
            }
        } catch(e) {}
    }
    checkTime();
    setInterval(checkTime, 10000);

    if (window.EventSource) {
        var es = new EventSource('/events');
        es.onmessage = function(e) {
            if (e.data === 'reload') {
                reloadPage();
            }
        };
    }
})();
</script>
</body>
`

	if strings.Contains(htmlStr, "</head>") {
		htmlStr = strings.Replace(htmlStr, "</head>", customHead+"</head>", 1)
	} else {
		htmlStr = customHead + htmlStr
	}

	if strings.Contains(htmlStr, "</body>") {
		htmlStr = strings.Replace(htmlStr, "</body>", customBottom, 1)
	} else {
		htmlStr = htmlStr + customBottom
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(htmlStr))
}
