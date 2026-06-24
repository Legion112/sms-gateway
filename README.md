# SMS Gateway

SMS gateway for receiving SMS messages on Linux using a **Quectel EC25-EUX** LTE modem connected over USB.

Single static binary with subcommands — ideal for headless Raspberry Pi deployment.

```bash
make
./bin/sms-gateway ping
./bin/sms-gateway status
./bin/sms-gateway ports
```

The project uses a pluggable **driver** model to talk to the modem:

| Driver | Backend | Default |
|--------|---------|---------|
| **`mm`** | ModemManager over system D-Bus | **yes** |
| **`serial`** | Direct AT commands on `/dev/ttyUSB*` | no |

## Architecture

```mermaid
flowchart TD
  Main["cmd/sms-gateway"] --> CLI["internal/cli"]
  CLI --> Config["internal/config"]
  Config --> Factory["internal/factory"]
  Factory -->|driver=mm| MM["internal/driver/mm"]
  Factory -->|driver=serial| Serial["internal/driver/serial"]
  MM --> DBus["ModemManager D-Bus"]
  Serial --> TTY["/dev/ttyUSB2 or ttyUSB3"]
```

Both drivers implement `internal/modem.Modem` (`Ping`, `SMSStatus`, `ListMessages`, `SendMessage`, `Close`).

Forward channels (Telegram, email, SMS, …) implement `internal/forward.Channel` with the same factory pattern as modem drivers.

## Commands

| Command | Description |
|---------|-------------|
| `ping` | Check modem connectivity |
| `status` | Show SIM and SMS readiness |
| `messages` | List all SMS messages |
| `send` | Send an SMS (`--number`, `--text`) |
| `modems list` | List configured modems; `--discover` shows ModemManager indices |
| `channel test` | Send a test message via a forward channel (e.g. Telegram) |
| `ports` | List detected serial ports |

Global flags (all subcommands):

```
--config string    Config file path
--driver string    Driver override: mm | serial
-v, --verbose      Verbose logging
```

Subcommand flags (`ping`, `status`, `messages`, `send`):

```
--modem string        Named modem from config modems map
--device string       Serial device (serial driver only)
--timeout duration    Command timeout
--modem-index int     ModemManager modem index (mm driver only)
```

### Multi-modem setups

When the `modems` section is configured (see [`config.example.yaml`](config.example.yaml)), `ping`, `status`, and `messages` run against **all** configured modems by default. Use `--modem NAME` to target one modem.

```bash
./bin/sms-gateway ping                         # all modems
./bin/sms-gateway status --modem ec25-main     # one modem
./bin/sms-gateway modems list --discover       # help map mmcli indices to config
```

For `send`, specify `--modem` when multiple modems are configured (or set `default_modem` in config).

Without a `modems` section, commands use the legacy top-level `driver` / `mm` / `serial` settings (single modem).

## Hardware

| Component | Details |
|-----------|---------|
| Modem | Quectel EC25-EUX (USB VID `2c7c`, PID `0125`) |
| Connection | USB to host computer |
| SIM | Required for SMS later; **not** required for ping PoC |

Typical USB interfaces exposed by the EC25 on Linux:

| Device | Purpose |
|--------|---------|
| `/dev/ttyUSB0` | Diagnostic (DM) |
| `/dev/ttyUSB1` | GPS NMEA |
| `/dev/ttyUSB2` | AT commands (primary) |
| `/dev/ttyUSB3` | AT / PPP |
| `/dev/cdc-wdm*` | QMI |
| `wwan0` | Network interface |

## Configuration

Copy the example config:

```bash
cp config.example.yaml config.yaml
```

Example [`config.example.yaml`](config.example.yaml):

```yaml
driver: mm   # mm | serial

serial:
  device: auto
  baud_rate: 115200
  timeout: 2s

mm:
  modem_index: 0
  timeout: 5s
```

**Precedence** (highest wins): CLI flags → environment variables → YAML file → built-in defaults.

| Setting | YAML | Environment | CLI flag |
|---------|------|-------------|----------|
| Config file | — | `SMS_GATEWAY_CONFIG` | `--config` |
| Driver | `driver` | `SMS_GATEWAY_DRIVER` | `--driver` |
| Serial device | `serial.device` | `MODEM_DEVICE` | `--device` |
| Timeout | `serial.timeout` / `mm.timeout` | `MODEM_TIMEOUT` | `--timeout` |
| MM modem index | `mm.modem_index` | `MODEM_INDEX` | `--modem-index` |
| Named modem | `modems.<name>` | — | `--modem` |

Config search order: `--config` path → `./config.yaml` → `/etc/sms-gateway/config.yaml` (skipped if missing).

### Forwarding

Optional sections in [`config.example.yaml`](config.example.yaml):

| Section | Purpose |
|---------|---------|
| `modems` | Named modems for multi-SIM setups (hardware settings) |
| `channels` | Named forward destinations (Telegram bot, email, SMS, …) |
| `forward_rules` | Route inbound SMS: `modem` + `from` → `to: [channels]` (first match wins) |

**Channel drivers:**

| Driver | Status |
|--------|--------|
| `telegram_bot` | Implemented — Bot API private chat |
| `telegram_secret` | Planned — Secret Chat (E2E) |
| `email` | Planned |
| `sms` | Planned — forward by sending SMS to another number |

**Telegram setup:**

1. Create a bot via [@BotFather](https://t.me/BotFather) → copy `bot_token`.
2. Open a chat with the bot and send `/start`.
3. Get your `chat_id` (`curl "https://api.telegram.org/bot<TOKEN>/getUpdates"` after `/start`).
4. Set token via env (recommended): `SMS_GATEWAY_CHANNEL_MY_TELEGRAM_BOT_TOKEN` or `SMS_GATEWAY_TELEGRAM_BOT_TOKEN`.

**Test channel:**

```bash
sms-gateway channel test my-telegram
sms-gateway channel test my-telegram --text "Hello from Pi" -v
```

**Security:** Bot API uses TLS to Telegram, but SMS text is **visible to Telegram** (cloud chat, not Secret Chat). For maximum privacy, a future `telegram_secret` driver is planned.

### Watch daemon (`sms-gateway-watch`)

Separate long-running binary: listen for incoming SMS on all `modems`, persist to SQLite, apply `forward_rules`, fan-out to `channels`.

```bash
go build -o bin/sms-gateway-watch ./cmd/sms-gateway-watch
./bin/sms-gateway-watch -v
```

Requires `modems`, `channels`, `forward_rules`, and `storage.path` in config (see [`config.example.yaml`](config.example.yaml)).

| Setting | Description |
|---------|-------------|
| `storage.path` | SQLite database for messages and delivery state |
| `watch.catch_up_on_start` | Forward existing inbound SMS on startup |
| `watch.serial_poll_interval` | Poll interval for `serial` modems (default 10s) |

**Modem backends:**

| Driver | Detection |
|--------|-----------|
| `mm` | ModemManager D-Bus `Messaging.Added` signal |
| `serial` | Poll `ListMessages` (cannot run while MM holds the same port) |

**systemd:**

```bash
sudo cp bin/sms-gateway-watch /usr/local/bin/
sudo cp deploy/systemd/sms-gateway-watch.service /etc/systemd/system/
sudo systemctl enable --now sms-gateway-watch
```

## Prerequisites

- Linux with Quectel USB drivers (`option`, `qmi_wwan`)
- Go **1.26** or newer
- **`mm` driver:** `ModemManager` running (`systemctl status ModemManager`)
- **`serial` driver:** user in `dialout` group

### Install Go 1.26

```bash
curl -fsSL -o /tmp/go1.26.0.linux-amd64.tar.gz https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
mkdir -p ~/.local/go1.26 && tar -C ~/.local/go1.26 --strip-components=1 -xzf /tmp/go1.26.0.linux-amd64.tar.gz
export PATH=$HOME/.local/go1.26/bin:$PATH
go version
```

### Serial port permissions (serial driver only)

```bash
sudo usermod -aG dialout $USER
# log out and back in
```

## Quick start

```bash
# Build
go build -o bin/sms-gateway ./cmd/sms-gateway

# Ping modem (default: ModemManager driver)
./bin/sms-gateway ping

# Check SIM and SMS readiness
./bin/sms-gateway status

# List all SMS messages
./bin/sms-gateway messages

# Send an SMS
./bin/sms-gateway send --number +1234567890 --text "Hello"

# Serial AT driver with verbose logging
./bin/sms-gateway --driver serial ping -v

# Env override
SMS_GATEWAY_DRIVER=serial ./bin/sms-gateway ping

# List serial ports
./bin/sms-gateway ports

# Help
./bin/sms-gateway --help
./bin/sms-gateway ping --help
```

Development without installing:

```bash
go run ./cmd/sms-gateway ping
go run ./cmd/sms-gateway status
```

Expected output (`ping`, MM driver):

```
driver: mm
device: /org/freedesktop/ModemManager1/Modem/0
status: ok
detail: QUALCOMM INCORPORATED QUECTEL Mobile Broadband Module (IMEI ..., state ...)
```

Expected output (`status`, MM driver):

```
driver: mm
device: /org/freedesktop/ModemManager1/Modem/0
sim: missing
modem: failed (sim-missing)
network: unavailable
messages: unknown
sms_ready: false
detail: sim=missing, modem=failed (sim-missing), network=unavailable
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Command succeeded |
| 1 | Modem operation failed |
| 2 | Setup failure (config, permissions, driver init) |

## Driver comparison

| | **mm** (default) | **serial** |
|--|------------------|------------|
| Best for | Headless Pi, desktop with MM | Dedicated gateway, full AT control |
| Permissions | D-Bus / polkit (usually works for session user) | `dialout` group |
| Port busy issue | No serial port opened | May need `auto` fallback or udev rule |
| Pi OS Bookworm | Works out of the box | Works with `dialout` |

## Troubleshooting

### MM driver: modem not found

- Check ModemManager: `systemctl status ModemManager`
- List modems: `mmcli -L`
- Try explicit path in config: `mm.modem_path: /org/freedesktop/ModemManager1/Modem/0`

### Serial driver: Serial port busy

ModemManager holds `/dev/ttyUSB2`. Use default `auto` device probing (tries `ttyUSB2` then `ttyUSB3`) or install the udev rule:

```bash
sudo cp deploy/udev/99-quectel-at.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger
```

Or use the **mm** driver (default) to avoid serial port contention entirely.

### SIM not detected (`sim-missing`)

Blocks SMS but not ping. Reseat the nano-SIM and check `mmcli -m 0 | grep -i sim`.

## Project layout

```
cmd/sms-gateway/          Single binary entrypoint
internal/cli/             Cobra commands (ping, status, messages, send, ports)
internal/cmdutil/         Shared flags and modem helpers
internal/config/          YAML + env + flag loading
internal/modem/           Modem interface and types
internal/factory/         Driver factory
internal/driver/mm/       ModemManager D-Bus driver (Linux)
internal/driver/serial/   Direct AT serial driver
internal/forward/         Forward channel interface, router, factory
internal/forward/telegrambot/  Telegram Bot API driver
internal/storage/         SQLite message and delivery store
internal/watch/           Watch daemon orchestration
cmd/sms-gateway-watch/    Watch daemon binary
config.example.yaml       Example configuration
deploy/systemd/           systemd unit for watch daemon
deploy/udev/              Optional udev rules for serial driver
```

## Roadmap

1. **PoC (done)** — `ping`, `status`, `messages`, `send`
2. **Forward channels (done)** — pluggable channels, routing rules, `channel test`, `telegram_bot`
3. **Watch daemon (done)** — `sms-gateway-watch`, SQLite persistence, MM D-Bus + serial poll
4. More channel drivers — `telegram_secret`, `email`, `sms`
5. HTTP/API gateway — expose SMS to other services

## License

Private project — license TBD.
