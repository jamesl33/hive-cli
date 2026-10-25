#!/bin/sh
# Install the heating schedule on the Pi. Run from this directory: sudo ./install.sh
# Expects the hive binary at /usr/local/bin/hive already (see README.md).

set -eu

if [ "$(id -u)" -ne 0 ]; then
	echo "run with sudo" >&2
	exit 1
fi

if ! command -v hive >/dev/null 2>&1; then
	echo "hive not found on PATH; copy the ARMv6 build to /usr/local/bin/hive first" >&2
	exit 1
fi

install -m 755 hive-schedule /usr/local/bin/hive-schedule
install -d -m 755 /etc/hive

# Don't clobber an edited schedule or credentials on reinstall
[ -e /etc/hive/schedule.weekday ] || install -m 644 schedule.example /etc/hive/schedule.weekday
[ -e /etc/hive/schedule.weekend ] || install -m 644 schedule.example /etc/hive/schedule.weekend
[ -e /etc/hive/env ] || install -m 600 env.example /etc/hive/env

install -m 644 hive-schedule.service /etc/systemd/system/hive-schedule.service
install -m 644 hive-schedule.timer /etc/systemd/system/hive-schedule.timer

# The history is readable by everyone, so any user can run 'hive dashboard --history /var/lib/hive'
id hive >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin hive
install -d -m 755 -o hive -g hive /var/lib/hive
install -m 644 hive-record.service /etc/systemd/system/hive-record.service

install -m 755 hive-presence /usr/local/bin/hive-presence
install -m 644 hive-presence.service /etc/systemd/system/hive-presence.service
install -m 644 hive-presence.timer /etc/systemd/system/hive-presence.timer
command -v arp-scan >/dev/null 2>&1 || echo "arp-scan not found; install it for HIVE_AWAY_MAC to work" >&2

# Make time-sync.target wait for a real NTP sync rather than being reached immediately
systemctl enable --now systemd-time-wait-sync.service 2>/dev/null || true

systemctl daemon-reload
systemctl enable --now hive-schedule.timer
systemctl enable --now hive-record.service
systemctl enable --now hive-presence.timer

# Apply the current slot straight away
systemctl start hive-schedule.service

systemctl list-timers hive-schedule.timer --no-pager
