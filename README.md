# OLT Monitor

<p align="center">
  <img src="./icon.png" width="128" alt="OLT Monitor">
</p>

<p align="center">
  A terminal (TUI) application that continuously monitors your OLTs and sends
  WhatsApp alerts when an OLT goes down or recovers.
</p>

<p align="center">
  <strong>Monitor your network. Get instant alerts. Control it remotely.</strong>
</p>

---

## ✨ Features

* 📡 Continuously ping multiple OLTs
* 🚨 WhatsApp alerts when an OLT goes **DOWN**
* ✅ WhatsApp alerts when an OLT **RECOVERS**
* 💬 Remotely control the monitor through WhatsApp
* 🖥️ Beautiful terminal dashboard built with Bubble Tea
* ⚡ Concurrent OLT monitoring
* 💾 Persistent configuration and WhatsApp session
* 🔄 Automatic WhatsApp reconnect
* 🔐 Admin-only WhatsApp commands
* 📱 WhatsApp linking through QR code or phone/pairing code
* ⚙️ Configurable monitoring interval
* 🎯 Manual ping from both TUI and WhatsApp

---

## 🏗️ Architecture

```text
                     ┌─────────────────────┐
                     │     Bubble Tea TUI   │
                     └──────────┬──────────┘
                                │
                                │
                     ┌──────────▼──────────┐
                     │    Config Manager    │
                     │      oltconfig       │
                     └──────────┬──────────┘
                                │
                  ┌─────────────┴─────────────┐
                  │                           │
          ┌───────▼────────┐        ┌────────▼────────┐
          │ Monitoring      │        │ WhatsApp        │
          │ Engine          │        │ Commands        │
          │                 │        │                 │
          │ Ping Workers    │        │ status/add/...  │
          │ Alert Manager   │        └────────┬────────┘
          └───────┬─────────┘                 │
                  │                           │
                  └─────────────┬─────────────┘
                                │
                     ┌──────────▼──────────┐
                     │ WhatsApp Client     │
                     │      whatsmeow       │
                     └─────────────────────┘
```

---

## 📁 Project Structure

```text
olt-monitor/
├── cmd/
│   └── olt-monitor/
│       └── main.go
│
├── internal/
│   ├── commands/
│   │   └── ...
│   │
│   ├── monitor/
│   │   └── ...
│   │
│   ├── oltconfig/
│   │   └── ...
│   │
│   ├── tui/
│   │   └── ...
│   │
│   └── whatsapp/
│       ├── client.go
│       ├── login.go
│       └── bridge.go
│
├── icon.png
├── go.mod
├── go.sum
└── README.md
```

---

## 📦 Build

Make sure Go is installed, then:

```bash
go mod tidy
```

Build the application:

### Linux / macOS

```bash
go build -o olt-monitor ./cmd/olt-monitor
```

### Windows

```powershell
go build -o olt-monitor.exe ./cmd/olt-monitor
```

The resulting executable can be copied to another computer without installing Go.

---

## 🚀 Run

### Linux / macOS

```bash
./olt-monitor
```

### Windows

```powershell
.\olt-monitor.exe
```

The application starts directly in the terminal and opens the OLT monitoring dashboard.

---

## 🗂️ Data & Configuration

By default, application data is stored in:

```text
./data/
```

This includes:

```text
data/
├── olts.json
└── WhatsApp session data
```

You can override the data directory with:

```bash
OLT_MONITOR_DATA=/path/to/data
```

### ⚠️ Keep the data directory private

The WhatsApp session contains authentication credentials and should **not** be shared or committed to Git.

Add it to `.gitignore`:

```gitignore
data/
```

---

## 🖥️ First Run

When you launch OLT Monitor for the first time:

1. The TUI opens with an empty OLT table.
2. Press **`A`** to add an OLT.
3. Enter the OLT name.
4. Enter its IP address.
5. Press **`W`** to link WhatsApp.
6. Choose the available WhatsApp linking method.
7. Enter your phone number when requested.
8. Complete the WhatsApp linking process.
9. Enter the administrator phone number.

The configured administrator receives OLT status alerts and is allowed to control the monitor through WhatsApp.

---

## 🎛️ TUI Controls

| Key       | Action                        |
| --------- | ----------------------------- |
| `↑` / `↓` | Move selection                |
| `j` / `k` | Move selection                |
| `A`       | Add OLT                       |
| `E`       | Edit selected OLT             |
| `D`       | Enable / disable selected OLT |
| `R`       | Remove selected OLT           |
| `P`       | Ping selected OLT             |
| `I`       | Change monitoring interval    |
| `W`       | Link WhatsApp / change admin  |
| `Q`       | Quit                          |

---

## 💬 WhatsApp Remote Control

The configured administrator can control OLT Monitor remotely.

### Check status

```text
status
```

Lists all configured OLTs and their current state.

### Add an OLT

```text
add <name> <ip>
```

Example:

```text
add OLT-Haripur 192.168.1.10
```

### Remove an OLT

```text
remove <name>
```

Example:

```text
remove OLT-Haripur
```

### Enable monitoring

```text
enable <name>
```

### Disable monitoring

```text
disable <name>
```

### Change monitoring interval

```text
interval <value>
```

Examples:

```text
interval 30
interval 30s
interval 5m
```

A bare number is interpreted as seconds.

### Ping an OLT immediately

```text
ping <name>
```

### Show available commands

```text
help
```

---

## 🚨 WhatsApp Alerts

OLT Monitor automatically sends an alert when an OLT changes state.

### OLT goes down

The administrator receives a **DOWN** notification.

### OLT recovers

When connectivity returns, the administrator receives a **RECOVERED** notification.

Alerts are generated on state transitions, preventing repeated messages while an OLT remains in the same state.

---

## 🔐 WhatsApp Security

Only the configured administrator number can execute remote commands.

Messages from other numbers are ignored.

The WhatsApp integration uses the same `whatsmeow`-based client and persistent session architecture used by the existing `nightcode-whatsapp` project.

The WhatsApp client implementation is reused from that project, while the OLT monitoring, command handling, configuration, alerting, and TUI are implemented specifically for OLT Monitor.

---

## 📡 OLT Monitoring

Each enabled OLT is monitored continuously.

The monitoring engine:

1. Runs checks at the configured interval.
2. Pings OLTs concurrently.
3. Tracks the current state of each OLT.
4. Detects UP → DOWN transitions.
5. Detects DOWN → UP recovery transitions.
6. Sends the corresponding WhatsApp alert.

Multiple OLTs can therefore be monitored simultaneously without blocking one another.

---

## 🐧 Linux ICMP Permissions

OLT Monitor uses unprivileged ICMP/UDP-based pinging.

On Linux, this may require enabling unprivileged ping:

```bash
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
```

To make the setting permanent:

```bash
echo 'net.ipv4.ping_group_range = 0 2147483647' \
  | sudo tee -a /etc/sysctl.conf
```

Alternatively, the binary can be granted the required capability:

```bash
sudo setcap cap_net_raw=+ep ./olt-monitor
```

---

## 🧩 Technologies

* **Go**
* **Bubble Tea**
* **Lip Gloss**
* **whatsmeow**
* **pro-bing**
* **JSON-based persistent configuration**

---

## 🔄 WhatsApp Integration

OLT Monitor uses the WhatsApp client from the existing `nightcode-whatsapp` project.

The following client files are reused:

```text
internal/whatsapp/client.go
internal/whatsapp/login.go
```

The WhatsApp bridge adds the functionality required by OLT Monitor, including:

* Sending messages to the configured administrator
* Event fan-out
* Connecting WhatsApp events to the command processor

This allows the same WhatsApp account to both:

* Receive monitoring alerts
* Remotely control OLT Monitor

---

## 💡 Example Workflow

```text
                    OLT Monitor
                         │
                ┌────────┴────────┐
                │                 │
             OLT #1            OLT #2
                │                 │
              PING              PING
                │                 │
              UP ✅             DOWN ❌
                                  │
                                  ▼
                           WhatsApp Alert
                                  │
                                  ▼
                            Administrator
                                  │
                         "status" / "ping"
                                  │
                                  ▼
                           OLT Monitor
```

---

## 📌 Current Status

**Fully working and tested.**

The application currently supports:

* OLT monitoring
* Concurrent pinging
* UP/DOWN state detection
* Recovery detection
* WhatsApp alerts
* WhatsApp remote commands
* Persistent configuration
* Persistent WhatsApp session
* WhatsApp reconnect behavior
* TUI management
* OLT add/edit/remove
* Enable/disable monitoring
* Configurable monitoring interval
* Manual OLT pinging
