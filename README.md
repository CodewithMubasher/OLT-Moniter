# OLT Monitor

A terminal (TUI) application that continuously pings your OLTs and sends
WhatsApp alerts when one goes down or recovers — with the same WhatsApp
account also able to remote-control the monitor (`status`, `add`, `remove`,
`enable`, `disable`, `interval`, `ping`).

## Quick start (client)

1. Clone this repo
2. Double-click `olt-monitor.exe`
3. Press **A** to add OLTs (name, then IP)
4. Press **W** to link WhatsApp (enter phone number, enter pairing code)
5. Done — it auto-reconnects on every restart, no need to link again

## Build from source (developer)

```bash
go mod tidy
go build -o olt-monitor.exe ./cmd/olt-monitor
```

## How it works

- Config and WhatsApp session live in `./data` next to the exe (auto-created)
- OLT config is saved to `data/olts.json`
- WhatsApp session is saved to `data/whatsapp.db`
- On restart, the app auto-detects a saved session and reconnects silently
- If WhatsApp invalidates the session, press **W** to re-link

## TUI keys

```
↑/↓ or j/k   Move selection
a            Add OLT
e            Edit selected OLT (name, then IP)
d            Enable/disable selected OLT
r            Remove selected OLT (confirm y/n)
i            Change monitoring interval (e.g. 10s, 30s, 5m, or a bare number of seconds)
w            Link WhatsApp / set admin number
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

## Pre-configuring for a client

Copy `data/olts.example.json` to `data/olts.json` and edit with the client's
OLTs and admin phone number. The client only needs to press **W** once to
link WhatsApp — everything else is already set up.

## Architecture

```
TUI ──────────────┬──────────────── WhatsApp commands
                   │                      │
              Config Manager (oltconfig) ─┘
                   │
            Monitoring Engine (monitor)
                   │
              Ping Workers (ICMP / TCP / HTTP)
                   │
             Alert Manager (monitor)
                   │
         WhatsApp Client (whatsmeow)
```

- `internal/oltconfig` — OLT list, interval, failure threshold, WhatsApp
  target: mutex-guarded, atomically-persisted JSON file (`data/olts.json`)
- `internal/monitor` — ping loop (concurrent per-OLT checks each tick)
  and alert manager for UP↔DOWN transitions
- `internal/commands` — parses WhatsApp text commands
- `internal/whatsapp` — WhatsApp client (whatsmeow-based session persistence)
- `internal/tui` — Bubble Tea dashboard
