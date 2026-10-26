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

`install.sh` seeds `/etc/hive/schedule.weekend` as a copy of the example; edit it for a different weekend. It replaces `schedule.weekday` all day Saturday and Sunday, and is optional (delete it to use the weekday schedule). Make the last slot of each file match so the changeover at midnight doesn't jump.

## Away floor

Set `HIVE_AWAY_MAC` in `/etc/hive/env` to your phone's Wi-Fi MAC (Android's per-network randomized one is stable) and install `arp-scan`. `hive-presence.timer` scans every 5 minutes and touches `/var/lib/hive/away-last-seen` when the phone answers. Once it has been unseen for `HIVE_AWAY_AFTER` seconds (default 1800), `hive-schedule` lowers any slot above `HIVE_AWAY_FLOOR` (default 14, or `off` to turn the heating off) to it; `off` slots stay off. When the phone reappears, `hive-presence` starts `hive-schedule` at once (within 5 minutes of your return) instead of waiting for the next slot. A missing file or failed scan counts as home.

## History

`hive-record.service` runs `hive record`, writing to `/var/lib/hive` for `hive dashboard --history /var/lib/hive`. It is built to go easy on the SD card:

- Changes only: a reading that rounds to the last one recorded (0.1°C, 1% humidity and battery) is dropped.
- Held in memory and appended to disk every 10 minutes. A power cut loses at most that.
- One file per UTC day, starting with the value of every series, so reading a day never needs another. Files older than 30 days are deleted; nothing is ever rewritten.
- Each change is one ~40 byte line. The dashboard reads all of it at start.

Check on it with `journalctl -u hive-record` and `ls -l /var/lib/hive`.

## Radiator valves

`hive-control.service` runs `hive control`. Each tick (5 minutes) it works out the opening of each room's TRV from the room sensor and the thermostat target: fully open until the room is 0.5°C below target, closing as it warms, down to 10%. It looks ahead using the rate the room is warming at, so the valve closes before the room reaches the target. A sensor silent for 30 minutes opens its valve fully. A valve is only written if its opening moved by 15% and it was not written in the last 5 minutes.

- `ExecStopPost=… --restore` opens every valve again when the service stops or crashes, as TRVs keep their last opening if it dies.
- Rooms are `--room name=sensor:valve`. `--observe` rooms are logged but never written (the lounge and stairs by default).
- It does nothing while the thermostat has no target (heating off).
- `journalctl -u hive-control -f` shows temperature, rate, wanted and current opening per room; `hive control --restore` opens every valve fully.

Not installed by `install.sh`. Install with `sudo install -m 644 hive-control.service /etc/systemd/system/ && sudo systemctl enable --now hive-control`.

## Day to day

```sh
sudo $EDITOR /etc/hive/schedule.weekday     # takes effect at the next slot
sudo systemctl start hive-schedule             # apply now
systemctl list-timers hive-schedule.timer      # next run
journalctl -u hive-schedule -n 20              # what was set and when
hive boost 1h 18                            # one-off; the next slot takes over
sudo systemctl disable --now hive-schedule.timer && hive schedule   # hand back to the thermostat
```
