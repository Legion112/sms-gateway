# SMS Gateway

SMS gateway for receiving SMS messages on Linux using a **Quectel EC25-EUX** LTE modem connected over USB.

The project talks to the modem over its AT command serial port. The first milestone is a minimal proof of concept: confirm the Go application can open the serial port and receive `OK` from a basic `AT` command.

## Hardware

| Component | Details |
|-----------|---------|
| Modem | Quectel EC25-EUX (USB VID `2c7c`, PID `0125`) |
| Connection | USB to host computer |
| AT port | `/dev/ttyUSB2` (115200 baud, 8N1) |
| SIM | Required for SMS later; **not** required for the AT ping PoC |

Typical USB interfaces exposed by the EC25 on Linux:

| Device | Purpose |
|--------|---------|
| `/dev/ttyUSB0` | Diagnostic (DM) |
| `/dev/ttyUSB1` | GPS NMEA |
| `/dev/ttyUSB2` | AT commands (primary) |
| `/dev/ttyUSB3` | AT / PPP |
| `/dev/cdc-wdm*` | QMI |
| `wwan0` | Network interface |

## Prerequisites

- Linux with Quectel USB drivers loaded (`option`, `qmi_wwan`)
- Go **1.26** or newer
- User in the `dialout` group (serial port access)

### Install Go 1.26

If your system Go is older, install 1.26 from [go.dev/dl](https://go.dev/dl/):

```bash
wget https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH
go version
```

Alternatively, install without sudo to `~/.local/go1.26`:

```bash
curl -fsSL -o /tmp/go1.26.0.linux-amd64.tar.gz https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
mkdir -p ~/.local/go1.26 && tar -C ~/.local/go1.26 --strip-components=1 -xzf /tmp/go1.26.0.linux-amd64.tar.gz
export PATH=$HOME/.local/go1.26/bin:$PATH
go version
```

Alternatively, with Go 1.21+ toolchain management, `go mod tidy` in this repo will auto-download Go 1.26 when `GOTOOLCHAIN=auto` (default).

### Serial port permissions

```bash
sudo usermod -aG dialout $USER
# log out and back in, or run: newgrp dialout
```

### ModemManager

ModemManager may hold the AT port. For direct serial access during development:

**Option A — udev rule (preferred):** create `/etc/udev/rules.d/99-quectel-at.rules`:

```
# Ignore EC25 AT port so ModemManager does not claim it
SUBSYSTEM=="tty", ATTRS{idVendor}=="2c7c", ATTRS{idProduct}=="0125", ENV{ID_MM_PORT_IGNORE}="1", KERNEL=="ttyUSB2"
```

Then reload udev and reconnect the modem:

```bash
sudo udevadm control --reload-rules && sudo udevadm trigger
```

**Option B — temporary:** stop ModemManager while testing:

```bash
sudo systemctl stop ModemManager
```

## Quick start

```bash
# List detected serial ports
go run ./cmd/modem-ping -list-ports

# Ping the modem (default device: /dev/ttyUSB2)
go run ./cmd/modem-ping

# Verbose AT traffic on stderr
go run ./cmd/modem-ping -verbose

# Custom device or timeout
go run ./cmd/modem-ping -device /dev/ttyUSB2 -timeout 3s
```

Build a binary:

```bash
go build -o bin/modem-ping ./cmd/modem-ping
./bin/modem-ping
```

Expected success output:

```
device: /dev/ttyUSB2
status: ok
response: OK
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Modem responded `OK` |
| 1 | Modem responded `ERROR` or unexpected/timeout response |
| 2 | Setup failure (permissions, port missing, list ports error) |

Environment variable `MODEM_DEVICE` overrides the default device path.

## Verify the modem is connected

```bash
lsusb | grep 2c7c:0125
mmcli -L
```

## Troubleshooting

### Permission denied on `/dev/ttyUSB*`

Add your user to `dialout` and start a new login session. Verify with `groups`.

### Timeout / no response

- Confirm the EC25 is plugged in: `lsusb | grep 2c7c`
- Use the correct AT port (`ttyUSB2`, not `ttyUSB1` which is GPS)
- Stop ModemManager or add the udev rule above
- Try `-verbose` to see raw TX/RX

### SIM not detected (`sim-missing`)

ModemManager may report `sim-missing`. This blocks SMS but **not** the AT ping PoC. Reseat the nano-SIM in the holder (correct orientation and alignment), then check:

```bash
mmcli -m 0 | grep -i sim
```

## Project layout

```
cmd/modem-ping/     CLI to verify modem connectivity
internal/modem/     AT serial client
```

## Roadmap

1. **PoC (current)** — `modem-ping`: open serial port, send `AT`, expect `OK`
2. SIM readiness — `AT+CPIN?`, signal and registration checks
3. SMS receive — configure `AT+CNMI`, handle `+CMTI` URCs, read messages with `AT+CMGR`
4. HTTP/API gateway — expose received SMS to other services
5. Production — systemd unit, logging, error recovery

For SMS handling, consider [github.com/warthog618/modem](https://github.com/warthog618/modem) as a higher-level AT driver built on `io.ReadWriter`.

## License

Private project — license TBD.
