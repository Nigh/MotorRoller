# MotorRoller

Host application for [TinyKnob](https://github.com/Nigh/TinyKnob) — live USB telemetry and motor mode control.

Built with [Wails v3](https://wails.io/) + Svelte + DaisyUI + Tailwind. Speaks the TinyKnob **Vendor Bulk** protocol ([docs/usb-protocol.md](https://github.com/Nigh/TinyKnob/blob/main/docs/usb-protocol.md)): VID `0xACDC`, PID `0x4011`.

## Features

- Auto-enumerate TinyKnob devices; pick and connect when several are present
- Real-time phase A/B/C duty values and scrolling waveforms
- Encoder angle (mrad / rad / deg) with a marked dial
- Mode buttons (incl. `STRESS` burn-in) highlight the live mode; BOOTLOADER is two-click
- Dial tracking (`GOTO` / `MOTOR_POS`): drag or scroll wheel to stream setpoints
- Spring controls: `SET_K` (stiffness) and `SET_REST`

## Requirements

- Go 1.25+
- Node.js + npm
- [Wails v3 CLI](https://v3.wails.io/) (`wails3`)
- libusb-1.0 (and headers for build)
- Linux: Wails defaults to **GTK4** + `webkitgtk-6.0`

```bash
# Debian/Ubuntu (GTK4 — default)
sudo apt install libusb-1.0-0-dev libgtk-4-dev libwebkitgtk-6.0-dev

# Optional GTK3 backend (only if you build with -tags gtk3)
# sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
```

### udev (Linux, non-root USB access)

Without this, Connect fails with `libusb: bad access [code -3]`:

```bash
sudo tee /etc/udev/rules.d/99-tinyknob.rules <<'EOF'
SUBSYSTEM=="usb", ATTR{idVendor}=="acdc", ATTR{idProduct}=="4011", MODE="0666", TAG+="uaccess"
EOF
sudo udevadm control --reload-rules
sudo udevadm trigger
```

Unplug/replug the device after installing the rule.

## Develop

```bash
# GTK4 (default)
wails3 dev

# Or
./build.sh dev
```

If the frontend builds but the window dies with WebKit/`bwrap` errors on Ubuntu 24.04+, disable the WebKit sandbox for local dev (see Troubleshooting).

Bindings are generated under `frontend/bindings/` (gitignored).

## Build

```bash
wails3 build
# or
./build.sh build
```

Binary: `build/bin/MotorRoller`.

Optional: `-tags gtk3` / `./build.sh build --gtk3` for the GTK3 WebKit backend; `-tags transparent` for frameless transparent (compositor-dependent, not required for this UI).

## Protocol

Vendor Bulk only (OUT `0x01`, IN `0x81`). CDC is unused by this host. Layout: [TinyKnob USB protocol](https://github.com/Nigh/TinyKnob/blob/main/docs/usb-protocol.md).

## Troubleshooting (Linux)

### `Failed to open display`

Wails needs a real GUI session. Run from a terminal inside your desktop (so `DISPLAY` or `WAYLAND_DISPLAY` is set). Headless / bare SSH shells will fail.

### `bwrap: setting up uid map: Permission denied` / `Failed to fully launch dbus-proxy` / `SIGTRAP`

WebKitGTK sandbox uses `bwrap`. Ubuntu often has `kernel.apparmor_restrict_unprivileged_userns=1`, which breaks it.

Dev workaround:

```bash
export WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1
wails3 dev
```

(Optional, wider impact: `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`.)

### `libusb: bad access [code -3]`

Install the udev rule above, reload, replug. Confirm with `lsusb -d acdc:4011` and that your user can open the device without root.

### Device lists but Connect fails / no Vendor interface

Confirm firmware exposes Vendor IF (class `0xFF`) with Bulk `0x01`/`0x81`:

```bash
lsusb -d acdc:4011 -v | grep -A20 'bInterfaceClass.*255'
```

Host code must claim with gousb `Interface(ifaceNumber, alternateSetting)` — use `alt.Alternate`, **not** `alt.Number` (`Number` is the interface id again). Wrong alt yields a failed claim that used to look like “vendor bulk interface not found”.

## License

MIT
