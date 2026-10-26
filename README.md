# rtl_sdr-weather_data_parser

Weather telemetry parser and live interactive dashboard for **EMOS E6016** & **E6016R** weather station sensors.

Supports two capture backends:
1. **Direct GPIO RF Receiver**: Pulse decoding via 433.92 MHz RF receiver module connected directly to Raspberry Pi GPIO (BCM 15 / Pin 10) using `pigpio`.
2. **RTL-SDR**: OOK PWM demodulation using `rtl_433` USB dongle.

---

## Architecture & Features

- **RF Pulse Decoder (`scripts/emos_gpio_rx.py`)**:
  - Samples OOK PWM signals with glitch filtering.
  - Bit-aligned sync preamble detection (`55 5a 7c` for weather station, `55 5a 75` for rain gauge).
  - Decodes temperature (0.1 °C resolution), relative humidity, wind speed (m/s & km/h), wind direction (16 compass points), rain total (0.7 mm resolution), battery state, and DCF77 radio atomic clock timestamps.
  - Verifies additive 8-bit checksum (`MIC`).
  - Appends JSON records to `graph.json`.

- **Web Dashboard (`main.go` / `wheater_graph`)**:
  - Full-width responsive layout (`100%` viewport width) with smooth ECharts visualization.
  - Distinct color palette for intuitive reading:
    - 🔴 **Temperature**: Warm Coral/Red (`#ff5722`)
    - 🔵 **Humidity**: Cool Sky/Cyan (`#00bcd4`)
    - 🟢 **Wind Speed**: Emerald Green (`#10b981`)
    - 🟣 **Rainfall**: Deep Indigo/Violet (`#7c3aed`)
  - **Live Auto-Reload**:
    - Uses Server-Sent Events (`/events`) to instantly trigger client reload the moment a new sensor measurement arrives.
    - Includes periodic polling fallback (`/last_time`) for network resilience.
  - Interactive timeline zoom (`DataZoom` slider) and crosshair inspection tooltip.

---

## Installation & Services Setup

To set up the automated services on Raspberry Pi OS / Debian:

```bash
sudo ./install_services.sh
```

This autonomously:
1. Installs `pigpio` daemon and Python dependencies.
2. Deploys `/usr/local/bin/emos_gpio_rx.py`.
3. Builds the Go web server binary (`wheater_graph`).
4. Installs and enables `emos-gpio-rx.service` and `wheater-graph.service`.
5. Starts the dashboard on port `8080` (accessible at `http://<ip>:8080`).

---

## Systemd Units

- **`emos-gpio-rx.service`**: Background 433 MHz decoder daemon.
  ```bash
  sudo systemctl status emos-gpio-rx.service
  sudo journalctl -u emos-gpio-rx.service -f
  ```
- **`wheater-graph.service`**: Dashboard web server.
  ```bash
  sudo systemctl status wheater-graph.service
  ```

---

## Manual Running

### GPIO RF Receiver
```bash
sudo systemctl start pigpiod
python3 scripts/emos_gpio_rx.py
```

### Web Server
```bash
go build -o wheater_graph
./wheater_graph -p 8080 -i graph.json
```
