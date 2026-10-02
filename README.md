# Bills OS

A tiny WhatsApp auto-reply bot for Windows. Link your WhatsApp account once,
set the message you want sent back, and Bills OS replies to every incoming
message automatically. Change the message any time from the app — it takes
effect on the very next message.

## What it does

- **Link WhatsApp** — pairing-code login (no QR scanning; enter an 8-character
  code from your phone). The session is saved, so it reconnects automatically
  on the next start.
- **Auto-reply** — every incoming message gets your configured reply.
  **Paused by default**: messages are received but never replied to until
  you resume it, so a reconnect after a long time can't blast old chats.
- **Simple TUI** — one screen: connection status + current reply message.

## Usage

```bash
go build -o bills-os.exe ./cmd/bills-os
./bills-os.exe
```

Keys:

| Key | Action                                   |
|-----|------------------------------------------|
| `w` | Link WhatsApp (first time / re-link)     |
| `m` | Edit the auto-reply message              |
| `t` | Pause / resume auto-reply                |
| `q` | Quit                                     |

Auto-reply starts **paused** — press `t` when you're ready to go live.

## Configuration

Settings live in `data/config.json` next to the executable (override the
folder with the `BILLS_OS_DATA` environment variable):

```json
{
  "whatsapp": {
    "auto_reply_message": "Thanks for your message! We have received it and will get back to you soon.",
    "auto_reply_paused": true
  }
}
```

`auto_reply_paused: true` means messages are received but no replies go out.

The linked WhatsApp session is stored in `data/whatsapp.db`. Delete it to
unlink the account.

## Build requirements

- Go 1.26+
- Windows (tested); the WhatsApp session database is per-machine
