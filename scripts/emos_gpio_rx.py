#!/usr/bin/env python3
"""
EMOS E6016 & E6016R 433.92MHz GPIO Pulse Decoder with Anomaly Detection & Fallback
Decodes OOK PWM signals from 433MHz receiver module on Raspberry Pi GPIO.
Default pin: BCM GPIO 15 (Physical Pin 10).
Outputs JSON records to graph.json with fault detection and Open-Meteo fallback.
"""

import sys
import os
import time
import json
import urllib.request
import urllib.error
import pigpio

GPIO_PIN = 15  # BCM 15 (Physical Pin 10)
OUTPUT_FILE = "/root/rtl_sdr-weather_data_parser/graph.json"

# Timing constants in microseconds
SHORT_MIN = 150
SHORT_MAX = 500
LONG_MIN = 550
LONG_MAX = 1100
RESET_MIN = 2000

PREAMBLE_WEATHER = f"{0x55:08b}{0x5a:08b}{0x7c:08b}"
PREAMBLE_RAIN    = f"{0x55:08b}{0x5a:08b}{0x75:08b}"

# Fallback Open-Meteo Configuration (Soběslavská Observatory, Prague 3)
OPEN_METEO_URL = (
    "https://api.open-meteo.com/v1/forecast"
    "?latitude=50.0877&longitude=14.4428"
    "&current=temperature_2m,relative_humidity_2m,precipitation,wind_speed_10m,wind_direction_10m"
    "&timezone=Europe/Prague"
)
FALLBACK_TIMEOUT = 300   # 5 minutes without valid station packet triggers fallback
FALLBACK_INTERVAL = 600  # Poll Open-Meteo every 10 minutes during fallback


class DataValidator:
    """Validates physical range bounds and rate-of-change (slew rate) spikes."""
    def __init__(self, output_path):
        self.output_path = output_path
        self.last_valid_temp = None
        self.last_valid_hum = None
        self.last_valid_rain = 0.0
        self.last_valid_wind = 0.0
        self.last_valid_station_time = time.time()
        self.consecutive_valid_station = 0
        self.is_fallback_active = False

        self._load_baseline()

    def _load_baseline(self):
        """Scans tail of output file to restore last known valid baseline."""
        if not os.path.exists(self.output_path):
            return
        try:
            with open(self.output_path, "r", encoding="utf-8") as f:
                lines = f.readlines()[-100:]
            for line in reversed(lines):
                line = line.strip()
                if not line:
                    continue
                try:
                    d = json.loads(line)
                    if self.last_valid_temp is None and d.get("temperature_C") is not None:
                        t = float(d["temperature_C"])
                        if -30.0 <= t <= 45.0:
                            self.last_valid_temp = t
                    if self.last_valid_hum is None and d.get("humidity") is not None:
                        h = int(d["humidity"])
                        if 10 <= h <= 100:
                            self.last_valid_hum = h
                    if d.get("rain_mm") is not None:
                        r = float(d["rain_mm"])
                        if r >= 0.0 and self.last_valid_rain == 0.0:
                            self.last_valid_rain = r
                    if self.last_valid_temp is not None and self.last_valid_hum is not None:
                        break
                except Exception:
                    pass
            print(f"[INIT] Baseline restored: Temp={self.last_valid_temp}°C, Hum={self.last_valid_hum}%, Rain={self.last_valid_rain}mm", flush=True)
        except Exception as e:
            print(f"[INIT] Notice: Could not read baseline from {self.output_path}: {e}", file=sys.stderr)

    def validate(self, record):
        now = time.time()
        model = record.get("model", "")

        if model in ("EMOS-E6016", "EMOS-E6018"):
            t = record.get("temperature_C")
            if t is None or not isinstance(t, (int, float)):
                return False, f"Missing or non-numeric temperature: {t}"
            if t < -30.0 or t > 45.0:
                return False, f"Absolute temperature limit exceeded: {t}°C"
            if self.last_valid_temp is not None and (now - self.last_valid_station_time) < 1200:
                delta = abs(t - self.last_valid_temp)
                if delta > 3.5:
                    return False, f"Temperature spike delta {delta:.1f}°C > 3.5°C (prev={self.last_valid_temp}°C, new={t}°C)"

            h = record.get("humidity")
            if h is None or not isinstance(h, int):
                return False, f"Missing or non-integer humidity: {h}"
            if h < 10 or h > 100:
                return False, f"Absolute humidity limit exceeded: {h}%"
            if self.last_valid_hum is not None and (now - self.last_valid_station_time) < 1200:
                delta = abs(h - self.last_valid_hum)
                if delta > 35:
                    return False, f"Humidity spike delta {delta}% > 35% (prev={self.last_valid_hum}%, new={h}%)"

            w = record.get("wind_avg_m_s")
            if w is not None and (w < 0.0 or w > 50.0):
                return False, f"Wind speed limit exceeded: {w} m/s"

            return True, "OK"

        elif model == "EMOS-E6016R":
            r = record.get("rain_mm")
            if r is None or r < 0.0:
                return False, f"Rain reading negative or missing: {r}"
            return True, "OK"

        return False, f"Unknown sensor model: {model}"

    def on_valid_station_record(self, record):
        now = time.time()
        self.last_valid_station_time = now
        self.consecutive_valid_station += 1

        if record.get("temperature_C") is not None:
            self.last_valid_temp = record["temperature_C"]
        if record.get("humidity") is not None:
            self.last_valid_hum = record["humidity"]
        if record.get("rain_mm") is not None:
            self.last_valid_rain = record["rain_mm"]
        if record.get("wind_avg_m_s") is not None:
            self.last_valid_wind = record["wind_avg_m_s"]

        if self.is_fallback_active and self.consecutive_valid_station >= 3:
            self.is_fallback_active = False
            print("[SOURCE] Station recovered! Re-established EMOS hardware as primary source.", flush=True)

    def on_fallback_record(self, record):
        if record.get("temperature_C") is not None:
            self.last_valid_temp = record["temperature_C"]
        if record.get("humidity") is not None:
            self.last_valid_hum = record["humidity"]
        if record.get("wind_avg_m_s") is not None:
            self.last_valid_wind = record["wind_avg_m_s"]


class FallbackWeatherProvider:
    """Fetches real-time weather from Open-Meteo API when station is degraded or offline."""
    @staticmethod
    def fetch(last_rain_mm):
        req = urllib.request.Request(OPEN_METEO_URL, headers={"User-Agent": "Observatory-Weather-Fallback/1.0"})
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                data = json.loads(resp.read().decode("utf-8"))
            cur = data.get("current", {})
            temp = round(float(cur.get("temperature_2m", 0.0)), 1)
            hum = int(cur.get("relative_humidity_2m", 50))
            wind_kmh = float(cur.get("wind_speed_10m", 0.0))
            wind_ms = round(wind_kmh * 0.27777777777778, 3)
            wind_dir = round(float(cur.get("wind_direction_10m", 0.0)), 1)

            record = {
                "time": time.strftime("%Y-%m-%d %H:%M:%S"),
                "model": "Open-Meteo-Fallback",
                "source": "fallback:open-meteo",
                "id": 0,
                "channel": 1,
                "battery_ok": 1,
                "temperature_C": temp,
                "humidity": hum,
                "wind_avg_m_s": wind_ms,
                "wind_dir_deg": wind_dir,
                "rain_mm": last_rain_mm,
                "radio_clock": time.strftime("%Y-%m-%dT%H:%M:%S"),
                "mic": "FALLBACK"
            }
            return record
        except Exception as e:
            print(f"[FALLBACK ERROR] Open-Meteo fetch failed: {e}", file=sys.stderr)
            return None


class EmosDecoder:
    def __init__(self, pi, pin, output_path, validator):
        self.pi = pi
        self.pin = pin
        self.output_path = output_path
        self.validator = validator
        self.rise_tick = 0
        self.fall_tick = 0
        self.bits = []
        self.last_record_hash = None

        # Configure pin
        self.pi.set_mode(pin, pigpio.INPUT)
        self.pi.set_pull_up_down(pin, pigpio.PUD_OFF)
        self.pi.set_glitch_filter(pin, 100)

        self.cb = self.pi.callback(pin, pigpio.EITHER_EDGE, self._edge_cb)

    def _edge_cb(self, gpio, level, tick):
        if level == 1:
            # Rising edge: gap (space) has ended, mark starts
            if self.fall_tick != 0:
                gap = pigpio.tickDiff(self.fall_tick, tick)
                if gap > RESET_MIN:
                    if len(self.bits) >= 72:
                        self.process_packet(self.bits)
                    self.bits = []
            self.rise_tick = tick
        elif level == 0:
            # Falling edge: mark (high pulse) has ended
            if self.rise_tick != 0:
                pulse = pigpio.tickDiff(self.rise_tick, tick)
                if SHORT_MIN <= pulse <= SHORT_MAX:
                    self.bits.append(0)
                elif LONG_MIN <= pulse <= LONG_MAX:
                    self.bits.append(1)
            self.fall_tick = tick

    def process_packet(self, bit_list):
        for inv in [True, False]:
            bit_str = "".join("0" if (b if not inv else 1 - b) == 0 else "1" for b in bit_list)

            # 1. Search for Weather Station Preamble (55 5a 7c)
            start = 0
            while True:
                idx = bit_str.find(PREAMBLE_WEATHER, start)
                if idx == -1:
                    break
                if idx + 120 <= len(bit_str):
                    chunk = bit_str[idx : idx + 120]
                    b_raw = [int(chunk[j : j + 8], 2) for j in range(0, 120, 8)]
                    b = [~x & 0xFF for x in b_raw]
                    chk = sum(b[:13]) & 0xFF
                    if chk == b[13]:
                        sensor_id = b[3]
                        variant = (b[4] >> 6) & 0x3
                        is_e6018 = (variant != 2)
                        battery = (b[12] >> 2) & 0x1

                        dcf77 = ((b[4] & 0x3F) << 26) | (b[5] << 18) | (b[6] << 10) | (b[7] << 2) | (b[8] >> 6)
                        dcf77_sec  = dcf77 & 0x3F
                        dcf77_min  = (dcf77 >> 6) & 0x3F
                        dcf77_hour = (dcf77 >> 12) & 0x1F
                        dcf77_day  = (dcf77 >> 17) & 0x1F
                        dcf77_mth  = (dcf77 >> 22) & 0x0F
                        dcf77_year = (dcf77 >> 26) & 0x3F

                        channel = ((b[8] >> 4) & 0x3) + 1
                        temp_raw = ((b[8] & 0x0F) << 8) | b[9]
                        if temp_raw & 0x800:
                            temp_raw -= 0x1000
                        temp_c = round(temp_raw * 0.1, 1)

                        humidity = b[10]
                        speed_ms = round(b[11] * 0.295, 3)
                        dir_raw = (b[12] & 0xF0) >> 4
                        dir_deg = round(dir_raw * 22.5, 1)

                        dcf77_str = f"{dcf77_year + 2000:04d}-{dcf77_mth:02d}-{dcf77_day:02d}T{dcf77_hour:02d}:{dcf77_min:02d}:{dcf77_sec:02d}"

                        record = {
                            "time": time.strftime("%Y-%m-%d %H:%M:%S"),
                            "model": "EMOS-E6018" if is_e6018 else "EMOS-E6016",
                            "source": "station",
                            "id": sensor_id,
                            "channel": channel,
                            "battery_ok": battery,
                            "temperature_C": temp_c,
                            "humidity": humidity,
                            "wind_avg_m_s": speed_ms,
                            "wind_dir_deg": dir_deg,
                            "radio_clock": dcf77_str,
                            "mic": "CHECKSUM"
                        }

                        # Anomaly / Spike Validation
                        valid, reason = self.validator.validate(record)
                        if not valid:
                            print(f"[FILTER] Outlier rejected from sensor {sensor_id}: {reason} | Packet: temp={temp_c}°C, hum={humidity}%", flush=True)
                            return

                        self.validator.on_valid_station_record(record)
                        self.save_record(record)
                        return
                start = idx + 1

            # 2. Search for Rain Gauge Preamble (55 5a 75)
            start = 0
            while True:
                idx = bit_str.find(PREAMBLE_RAIN, start)
                if idx == -1:
                    break
                if idx + 72 <= len(bit_str):
                    chunk = bit_str[idx : idx + 72]
                    b_raw = [int(chunk[j : j + 8], 2) for j in range(0, 72, 8)]
                    b = [~x & 0xFF for x in b_raw]
                    chk = sum(b[:8]) & 0xFF
                    if chk == b[8]:
                        sensor_id = b[3]
                        battery = (b[4] >> 6) & 1
                        rain_raw = ((b[6] & 0x0F) << 8) | b[7]
                        rain_mm = round(rain_raw * 0.7, 1)

                        record = {
                            "time": time.strftime("%Y-%m-%d %H:%M:%S"),
                            "model": "EMOS-E6016R",
                            "source": "station",
                            "id": sensor_id,
                            "battery_ok": battery,
                            "rain_mm": rain_mm,
                            "mic": "CHECKSUM"
                        }

                        valid, reason = self.validator.validate(record)
                        if not valid:
                            print(f"[FILTER] Outlier rejected from rain gauge {sensor_id}: {reason}", flush=True)
                            return

                        self.validator.on_valid_station_record(record)
                        self.save_record(record)
                        return
                start = idx + 1

    def save_record(self, record):
        # Deduplicate repeats within 30 seconds
        rec_hash = f"{record.get('id')}_{record.get('temperature_C')}_{record.get('humidity')}_{record.get('radio_clock')}_{record.get('rain_mm')}_{record.get('source')}"
        now = time.time()
        if self.last_record_hash and self.last_record_hash[0] == rec_hash and (now - self.last_record_hash[1]) < 30:
            return
        self.last_record_hash = (rec_hash, now)

        print(f"Decoded: {record}", flush=True)
        try:
            line = json.dumps(record) + "\n"
            with open(self.output_path, "a") as f:
                f.write(line)
        except Exception as e:
            print(f"Error saving record: {e}", file=sys.stderr)

    def cancel(self):
        self.cb.cancel()


def main():
    pi = pigpio.pi()
    if not pi.connected:
        print("Error: Could not connect to pigpio daemon.", file=sys.stderr)
        sys.exit(1)

    print(f"Starting EMOS GPIO RF Decoder on BCM GPIO {GPIO_PIN}...")
    validator = DataValidator(OUTPUT_FILE)
    decoder = EmosDecoder(pi, GPIO_PIN, OUTPUT_FILE, validator)

    last_fallback_poll = 0

    try:
        while True:
            time.sleep(1)
            now = time.time()

            # Check if station hasn't produced valid data for > FALLBACK_TIMEOUT
            if (now - validator.last_valid_station_time) > FALLBACK_TIMEOUT:
                if not validator.is_fallback_active:
                    validator.is_fallback_active = True
                    print(f"[FALLBACK] Station offline or degrading (> {FALLBACK_TIMEOUT}s without valid data). Activating Open-Meteo fallback.", flush=True)

                if (now - last_fallback_poll) >= FALLBACK_INTERVAL:
                    record = FallbackWeatherProvider.fetch(validator.last_valid_rain)
                    if record:
                        validator.on_fallback_record(record)
                        decoder.save_record(record)
                        last_fallback_poll = now
                        print(f"[FALLBACK] Open-Meteo recorded: Temp={record['temperature_C']}°C, Hum={record['humidity']}%, Wind={record['wind_avg_m_s']}m/s", flush=True)

    except KeyboardInterrupt:
        pass
    finally:
        decoder.cancel()
        pi.stop()


if __name__ == "__main__":
    main()
