#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=== Installing EMOS Weather Station & Graph Services ==="

# Check root privileges
if [[ $EUID -ne 0 ]]; then
   echo "Error: This script must be run as root (use sudo)."
   exit 1
fi

# 1. Install prerequisites (pigpio daemon and python3)
echo "Checking prerequisites..."
if command -v apt-get >/dev/null 2>&1; then
    apt-get update -y
    apt-get install -y pigpio python3-pigpio
fi

# 2. Enable & start pigpiod daemon
echo "Enabling and starting pigpiod service..."
systemctl enable pigpiod
systemctl start pigpiod

# 3. Install GPIO decoder script
echo "Installing /usr/local/bin/emos_gpio_rx.py..."
mkdir -p /usr/local/bin
cp -f "${SCRIPT_DIR}/scripts/emos_gpio_rx.py" /usr/local/bin/emos_gpio_rx.py
chmod +x /usr/local/bin/emos_gpio_rx.py

# 4. Build web server binary
echo "Building wheater_graph binary..."
(cd "${SCRIPT_DIR}" && go build -o wheater_graph)

# 5. Install systemd services
echo "Installing systemd service units..."
cp -f "${SCRIPT_DIR}/services/emos-gpio-rx.service" /etc/systemd/system/
cp -f "${SCRIPT_DIR}/services/wheater-graph.service" /etc/systemd/system/

systemctl daemon-reload

echo "Starting emos-gpio-rx and wheater-graph services..."
systemctl enable --now emos-gpio-rx.service
systemctl restart emos-gpio-rx.service

systemctl enable --now wheater-graph.service
systemctl restart wheater-graph.service

echo "=== Installation complete ==="
echo "Decoder status: $(systemctl is-active emos-gpio-rx.service)"
echo "Web server status: $(systemctl is-active wheater-graph.service)"
echo "Web dashboard listening on http://0.0.0.0:8080"
