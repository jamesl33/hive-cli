# Scheduled heating with systemd

Runs a heating schedule on the Pi: `hive-schedule.timer` fires every 15 minutes, and `hive-schedule` runs `hive manual <temp>` (or `hive off`) for the slot covering the current time.

- Re-running is harmless, so `Persistent=true` catches up once after the Pi was off.
- Waits for NTP sync first: the Pi Zero has no clock battery.
- Retries every 30 s for about 15 minutes if the broker or receiver doesn't confirm.
- Manual mode makes the receiver ignore the SLT3c schedule.

## Install

1. Build for the Pi Zero W (ARMv6) on another machine and copy it over:

   ```sh
   GOOS=linux GOARCH=arm GOARM=6 make build
   scp build/hive pi@<pi>:/tmp/hive
   ```

2. On the Pi:

   ```sh
   sudo install -m 755 /tmp/hive /usr/local/bin/hive
   hive status
   cd deploy/systemd && sudo ./install.sh
   ```

   If Mosquitto needs a login, put `HIVE_USERNAME` and `HIVE_PASSWORD` in `/etc/hive/env`.

`install.sh` won't overwrite an existing `/etc/hive/schedule` or `/etc/hive/env`.

## Day to day

```sh
sudo $EDITOR /etc/hive/schedule             # takes effect at the next slot
sudo systemctl start hive-schedule             # apply now
systemctl list-timers hive-schedule.timer      # next run
journalctl -u hive-schedule -n 20              # what was set and when
hive boost 1h 18                            # one-off; the next slot takes over
sudo systemctl disable --now hive-schedule.timer && hive schedule   # hand back to the thermostat
```
