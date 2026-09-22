# OLT Monitor

A terminal (TUI) application that continuously pings your OLTs and sends
WhatsApp alerts when one goes down or recovers — with the same WhatsApp
account also able to remote-control the monitor (`status`, `add`, `remove`,
`enable`, `disable`, `interval`, `ping`).

This project reuses the WhatsApp client from your existing **nightcode-whatsapp**
project (`internal/whatsapp/client.go` and `login.go` are copied in
**unmodified**) — same whatsmeow-based linking flow (QR code or phone/pairing
code), same session persistence, same reconnect behavior. Everything else
(OLT config, ping engine, alert formatting, WhatsApp command parsing, and the
dashboard TUI) is new.

## ⚠️ Important: this has not been compiled

This project was written in an environment without a Go toolchain or network
access to the Go module proxy, so **`go build` has not actually been run**.
The code was written carefully against the real source files from your
uploaded project and cross-checked against the exact API of every external
library used, but treat your first build as a debugging pass, not a
guarantee. See "If the build fails" below for the most likely issues.

## Build & run

```bash
cd olt-monitor
go mod tidy      # resolves/verifies all dependency versions — do this first
go build -o olt-monitor ./cmd/olt-monitor
./olt-monitor
```

Windows: `go build -o olt-monitor.exe ./cmd/olt-monitor`

Config, WhatsApp session, and message data live in `./data` (override with
`OLT_MONITOR_DATA=/path`). **Keep that folder private** — it contains your
WhatsApp session keys.

## First run

1. The TUI opens showing an empty OLT table.
2. Press **A** to add your first OLT (name, then IP).
3. Press **W** to link WhatsApp, enter your phone number, then enter the
   pairing code in WhatsApp's "Link with phone number" flow.
4. After linking, you'll be asked for an **admin phone number** — this is the
   number that receives DOWN/RECOVERED alerts and is allowed to send
   commands. You can change it later by pressing **W** again once linked.

## TUI keys

```
↑/↓ or j/k   Move selection
a            Add OLT
e            Edit selected OLT (name, then IP)
d            Enable/disable selected OLT
r            Remove selected OLT (confirm y/n)
p            Ping selected OLT immediately
i            Change monitoring interval (e.g. 10s, 30s, 5m, or a bare number of seconds)
w            Link WhatsApp with a pairing code / change admin number
q            Quit
```

## WhatsApp commands

Send these from the configured admin number:

```
status                  List all OLTs and their state
add <name> <ip>         Add and start monitoring
remove <name>           Remove an OLT
enable <name>           Resume monitoring
disable <name>          Pause monitoring
interval <n>            Change check interval (e.g. "interval 30", "interval 5m")
ping <name>             Check one OLT immediately
help                    List commands
```

Commands from any other number are ignored (the underlying client only
surfaces messages from the number set with `SetTarget`, exactly like
nightcode-whatsapp's own single-target design).

## Ping privileges (Linux)

This uses an "unprivileged" ICMP ping (UDP-based), which on Linux needs one
of the following:

```bash
# Recommended: allow unprivileged ping for all users
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"

# To make that permanent:
echo 'net.ipv4.ping_group_range = 0 2147483647' | sudo tee -a /etc/sysctl.conf
```

Or, alternatively, run the binary as root, or grant it the capability:

```bash
sudo setcap cap_net_raw=+ep ./olt-monitor
```

## If the build fails

Since this hasn't been compiled, here's where to look first:

- **Dependency version mismatches**: `go.mod` pins versions inherited from
  your original project plus `github.com/prometheus-community/pro-bing`. Run
  `go mod tidy` — it will correct any wrong indirect-dependency versions
  automatically.
- **whatsmeow API drift**: the reused `client.go`/`login.go` are verbatim
  copies from your working project, so they should compile as-is against the
  same whatsmeow version. If whatsmeow's API has moved since, the errors will
  point at those two files specifically — compare against your original
  nightcode-whatsapp checkout.
- **Everything else** (`oltconfig`, `monitor`, `commands`, `tui`) is new code
  using only the standard library plus bubbletea/lipgloss (same versions your
  project already uses) and pro-bing — these are lower-risk but worth
  checking first if errors point there instead.

## Architecture

```
TUI ──────────────┬──────────────── WhatsApp commands
                   │                      │
              Config Manager (oltconfig) ─┘
                   │
            Monitoring Engine (monitor)
                   │
              Ping Workers (pro-bing)
                   │
             Alert Manager (monitor)
                   │
         WhatsApp Client (whatsmeow, reused)
```

- `internal/oltconfig` — OLT list, interval, failure threshold, WhatsApp
  target: one mutex-guarded, atomically-persisted JSON file
  (`data/olts.json`), read and written by both the TUI and WhatsApp commands.
- `internal/monitor` — the ping loop (concurrent per-OLT checks each tick)
  and the alert manager that turns UP↔DOWN transitions into the exact
  message formats from the spec, sent at most once per state change.
- `internal/commands` — parses WhatsApp text commands and calls the same
  config/engine methods the TUI calls.
- `internal/whatsapp` — `client.go`/`login.go` unmodified from
  nightcode-whatsapp; `bridge.go` adds `SendTo` and an event fan-out so both
  the TUI and the command loop can each see every WhatsApp event.
- `internal/tui` — the Bubble Tea dashboard.
