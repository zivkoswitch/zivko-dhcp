# DHCP GUI

Native Ubuntu desktop application in Go with a native DHCP backend written in Go.

The project is operated through a single binary:

- `dhcp-gui`

The same binary supports multiple modes:

- default: GUI plus automatic daemon detection
- `--headless`: DHCP/control backend without GUI
- `--gui-only`: GUI without starting an embedded backend

If no daemon is already reachable on the control socket, the default GUI mode starts an embedded DHCP backend for as long as the window stays open. Closing the window stops the embedded backend again.

## Current DHCP Capability

The current Go DHCP implementation supports:

- DHCPDISCOVER to DHCPOFFER
- DHCPREQUEST to DHCPACK or DHCPNAK
- DHCPRELEASE handling
- lease allocation from configured pools
- exclusion ranges
- fixed reservations by MAC or hostname
- reuse of existing active leases for the same client
- pruning of expired leases
- router, DNS server, domain name, subnet mask and lease-time options

This is the current production-targeted baseline for single-server DHCP on Ubuntu. More advanced scenarios like relay-agent handling or multi-interface policy routing are not fully implemented yet.

## Production Overview

The intended production layout is:

- GUI binary: `/usr/local/bin/dhcp-gui`
- systemd unit: `/etc/systemd/system/dhcp-gui-daemon.service`
- Runtime socket: `/run/dhcp-gui/dhcp-gui.sock`
- Persistent config: `/var/lib/dhcp-gui/config.json`

The GUI talks to the backend over the local Unix socket. In service mode, systemd starts the same `dhcp-gui` binary with `--headless`.

Recommended production mode:

- install the release artifact
- let systemd run `dhcp-gui --headless`
- use `dhcp-gui --gui-only` on admin desktops that should only manage an already running service

## Build A Release Artifact

From the repository root:

```bash
./scripts/build-release.sh --version v0.1.0
```

This creates a release archive in `dist/` containing:

- `dhcp-gui`
- `dhcp-gui-daemon.service`
- `install.sh`
- `README.md`

The release archive is self-contained. The bundled `install.sh` is intended to be executed from the extracted archive directory.

## Install From The Release Artifact

Extract the archive and run the bundled installer:

```bash
tar -xzf dhcp-gui-linux-arm64.tar.gz
cd extracted-release-directory
./install.sh
```

The bundled `install.sh` is part of the release artifact and is the recommended production installation path.

It installs:

- `/usr/local/bin/dhcp-gui`
- `/etc/systemd/system/dhcp-gui-daemon.service`

Then it:

- creates `/var/lib/dhcp-gui`
- reloads systemd
- enables `dhcp-gui-daemon.service`
- restarts the daemon service

## Installer Options

Install from a specific tarball:

```bash
./install.sh --artifact ./dhcp-gui-linux-arm64.tar.gz
```

Install binaries into a different directory:

```bash
./install.sh --bin-dir ~/.local/bin
```

Skip GUI runtime packages:

```bash
./install.sh --skip-packages
```

## Start The Application

After installation, the daemon should already be enabled via systemd.

Start the GUI:

```bash
/usr/local/bin/dhcp-gui
```

Run the backend without GUI:

```bash
/usr/local/bin/dhcp-gui --headless
```

Run only the GUI and never start an embedded backend:

```bash
/usr/local/bin/dhcp-gui --gui-only
```

Check daemon status:

```bash
sudo systemctl status dhcp-gui-daemon.service
```

View daemon logs:

```bash
sudo journalctl -u dhcp-gui-daemon.service -f
```

## Example Modes

Desktop mode with automatic embedded backend if no service is available:

```bash
dhcp-gui
```

Desktop mode without starting a backend:

```bash
dhcp-gui --gui-only
```

Headless backend mode:

```bash
dhcp-gui --headless
```

Bind the DHCP server to a specific interface:

```bash
dhcp-gui --headless --interface enp2s0
```

## Operational Notes

- The daemon service is configured for UDP port `67`.
- The GUI uses the daemon control socket at `/run/dhcp-gui/dhcp-gui.sock`.
- Without `--gui-only`, the GUI auto-detects whether a backend is already reachable. If not, it starts an embedded backend in-process.
- The GUI can still keep a local cache, but the backend is the authoritative runtime component.
- The current implementation is intended for Ubuntu Linux.

## Maintainer Workflow

Install local build dependencies if needed:

```bash
sudo bash ./scripts/install-dev-tools.sh
```

Verify the codebase:

```bash
go build ./...
go test ./...
```
