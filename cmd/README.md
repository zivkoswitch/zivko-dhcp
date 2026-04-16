# DHCP GUI

Native desktop application in Go with a native DHCP backend written in Go.

The project is operated through a single binary:

- `zivko-dhcp`

The same binary supports multiple modes on Linux:

- default: GUI plus automatic daemon detection
- `--headless`: DHCP/control backend without GUI
- `--gui-only`: GUI without starting an embedded backend

If no backend is already reachable on the local control endpoint, the default GUI mode starts an embedded DHCP backend for as long as the window stays open. Closing the window stops the embedded backend again.

On Windows, the application is GUI-only. There is no service installation and no supported `--headless` mode. The DHCP server runs embedded in the GUI process.

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

This is the current production-targeted baseline for single-server DHCP. More advanced scenarios like relay-agent handling or multi-interface policy routing are not fully implemented yet.

## Linux Production Overview

The intended production layout is:

- GUI binary: `/usr/local/bin/zivko-dhcp`
- systemd unit: `/etc/systemd/system/zivko-dhcp-daemon.service`
- Runtime socket: `/run/zivko-dhcp/zivko-dhcp.sock`
- Persistent config: `/var/lib/zivko-dhcp/config.json`

On Linux, the GUI talks to the backend over the local control socket. In service mode, systemd starts the same `zivko-dhcp` binary with `--headless`.

Recommended production mode:

- install the release artifact
- let systemd run `zivko-dhcp --headless`
- use `zivko-dhcp --gui-only` on admin desktops that should only manage an already running service

## Build Release Artifacts

From the repository root:

```bash
./scripts/build-release.sh --version v0.1.0 --os linux --arch amd64
```

Linux artifacts now contain only the binary:

- `dist/zivko-dhcp-linux-amd64`

Windows artifacts can be built as:

```bash
./scripts/build-release.sh --version v0.1.0 --os windows --arch amd64
```

Windows artifacts now contain only the binary:

- `dist/zivko-dhcp-windows-amd64.exe`

For every artifact, the build also writes a `.sha256` checksum file next to the binary.

## Linux Service Example

An example systemd unit file is kept in the repository:

```bash
examples/zivko-dhcp-daemon.service
```

Copy it manually if you want to run the backend as a system service.

## Start The Application

Linux:

Start the GUI:

```bash
/usr/local/bin/zivko-dhcp
```

Run the backend without GUI:

```bash
/usr/local/bin/zivko-dhcp --headless
```

Run only the GUI and never start an embedded backend:

```bash
/usr/local/bin/zivko-dhcp --gui-only
```

Check daemon status:

```bash
sudo systemctl status zivko-dhcp-daemon.service
```

View daemon logs:

```bash
sudo journalctl -u zivko-dhcp-daemon.service -f
```

Windows:

```bash
zivko-dhcp.exe
```

## Example Modes

Desktop mode with automatic embedded backend if no service is available:

```bash
zivko-dhcp
```

Desktop mode without starting a backend:

```bash
zivko-dhcp --gui-only
```

Headless backend mode on Linux:

```bash
zivko-dhcp --headless
```

Bind the DHCP server to a specific interface:

```bash
zivko-dhcp --headless --interface enp2s0
```

## Operational Notes

- The daemon service is configured for UDP port `67`.
- On Linux, the GUI uses the daemon control socket at `/run/zivko-dhcp/zivko-dhcp.sock`.
- On Windows, the GUI uses a local control endpoint at `127.0.0.1:6768`.
- Without `--gui-only`, the GUI auto-detects whether a backend is already reachable. If not, it starts an embedded backend in-process.
- The GUI can still keep a local cache, but the backend is the authoritative runtime component.
- Linux service management is only available on Linux.

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
