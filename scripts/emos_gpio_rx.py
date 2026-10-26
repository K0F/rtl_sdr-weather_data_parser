#!/usr/bin/env python3
"""
EMOS E6016 & E6016R 433.92MHz GPIO Pulse Decoder
Decodes OOK PWM signals from 433MHz receiver module on Raspberry Pi GPIO.
Default pin: BCM GPIO 15 (Physical Pin 10).
Outputs JSON records compatible with rtl_433 to graph.json.
"""

import sys
import time
import json
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

class EmosDecoder:
    def __init__(self, pi, pin, output_path):
        self.pi = pi
        self.pin = pin
        self.output_path = output_path
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
                            "id": sensor_id,
                            "battery_ok": battery,
                            "rain_mm": rain_mm,
                            "mic": "CHECKSUM"
                        }
                        self.save_record(record)
                        return
                start = idx + 1

    def save_record(self, record):
        # Deduplicate repeats within 30 seconds
        rec_hash = f"{record.get('id')}_{record.get('temperature_C')}_{record.get('humidity')}_{record.get('radio_clock')}_{record.get('rain_mm')}"
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
    decoder = EmosDecoder(pi, GPIO_PIN, OUTPUT_FILE)

    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        pass
    finally:
        decoder.cancel()
        pi.stop()

if __name__ == "__main__":
    main()
