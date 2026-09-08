# chatTUI 💬

> A modern, lightweight, Discord/Slack-inspired terminal chat platform built from scratch in Go with Charm's Bubble Tea, Lip Gloss, SQLite persistence, and private networking over Tailscale.

```
┌───────────────────────────────────────────────────────────────────────────────┐
│ chatTUI                                                       ● Connected     │
├──────────────────────┬────────────────────────────────────────────────────────┤
│ CHATS                │ # global                                               │
│                      │ ────────────────────────────────────────────────────── │
│ ● **global**         │ 10:32  alex                                            │
│ ● **dev**            │        hey everyone! welcome to chatTUI                │
│   # announcements    │                                                        │
│                      │ 10:33  rtoms                                           │
│ DIRECT MESSAGES      │        what's up @alex? checking out the new rooms     │
│   @alex              │                                                        │
│   @maya              │ 10:34  ● maya joined #global                           │
│                      │                                                        │
│ ROOMS                │                                                        │
│   # coding           │                                                        │
│   # projects         │                                                        │
├──────────────────────┴────────────────────────────────────────────────────────┤
│ > Type a message or /help...                                                  │
├───────────────────────────────────────────────────────────────────────────────┤
│ Ctrl+K Search   Ctrl+N DM   Ctrl+R Room   Ctrl+P Profile   Ctrl+H Help   Ctrl+Q │
└───────────────────────────────────────────────────────────────────────────────┘
```

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Data Model & Persistence](#data-model--persistence)
- [Quickstart Guide](#quickstart-guide)
  - [Prerequisites](#prerequisites)
  - [Building from Source](#building-from-source)
  - [Starting the Server](#starting-the-server)
  - [Launching the Client](#launching-the-client)
- [Deploying with Tailscale on Ubuntu](#deploying-with-tailscale-on-ubuntu)
- [Docker Deployment](#docker-deployment)
- [Keyboard Shortcuts & Commands](#keyboard-shortcuts--commands)
- [Room Permissions Matrix](#room-permissions-matrix)
- [Development & Automated Testing](#development--automated-testing)
- [License](#license)

---

## Features

- 🖥️ **Full Multi-Pane TUI**: Built with Charm's [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), and [Lip Gloss](https://github.com/charmbracelet/lipgloss). Responsive to terminal window resizing.
- 🔐 **Secure Authentication**: Interactive INK-style registration flow on first launch. Passwords securely hashed with `bcrypt`. Session management with instant reconnect.
- ⚡ **Length-Prefixed Wire Protocol**: Robust framing over raw TCP, avoiding newline truncation and ensuring low-latency frame delivery over private networks.
- 🌐 **Global Chat & Public/Private Rooms**: Built-in `# global` channel, discoverable public rooms, and invite/password-protected private rooms.
- 🔒 **Password-Protected Rooms**: Rooms can be password protected with hashed secrets and interactive password prompts.
- ⏱️ **Temporary Ephemeral Rooms**: Channels can have an expiration lifetime (e.g. 6 hours). Automatic cleanup worker archives/deletes rooms and notifies members.
- 🛡️ **Authoritative Room Administration**: 4-tier server-enforced role hierarchy (`Owner` > `Admin` > `Moderator` > `Member`) with kick, ban, mute, role management, and ownership transfer confirmation dialogs.
- ✉️ **Direct Messaging (1-on-1)**: Private direct message channels with online status indicators and unread tracking.
- 🔔 **Mentions & Real-Time Notifications**: Detects `@username` mentions, highlights them visually, and delivers instant toast notifications.
- 🔴 **Visual Unread Indicators**: Distinct bold channel names with bright green `●` unread badges that automatically clear when opened.
- 📜 **Smooth Auto-Scroll & Scrollback**: Viewport locks to the latest message automatically when at bottom, pauses scrolling when navigating upward, and resumes at bottom.
- 🟢 **Presence Engine**: Live presence transitions (`● Online`, `◐ Idle`, `○ Away`, `○ Offline`) with automatic idle detection after inactivity.
- 🎨 **User Colors & Custom Profiles**: Choose custom display colors and personal status messages.
- 🧹 **24-Hour Message Retention**: Background cleanup ticker automatically purges messages older than 24 hours. Pure Go SQLite driver (`modernc.org/sqlite`) guarantees zero CGO dependency headaches.

---

## Architecture

```
                          ┌────────────────────────┐
                          │    SQLite Database     │
                          │  (modernc.org/sqlite)  │
                          │   WAL Mode / Indexes   │
                          └──────────▲─────────────┘
                                     │
                          ┌──────────┴─────────────┐
                          │     chatTUI Server     │
                          │                        │
                          │  • Auth & Sessions     │
                          │  • Room & Perm Engine  │
                          │  • Message Dispatcher  │
                          │  • Presence Tracker    │
                          │  • 24h Prune Worker    │
                          │  • Expiry Worker       │
                          └──────────▲─────────────┘
                                     │
                      Tailscale / TCP (Length-Prefixed JSON)
                                     │
               ┌─────────────────────┴─────────────────────┐
               │                                           │
    ┌──────────┴──────────┐                     ┌──────────┴──────────┐
    │    chatTUI Client   │                     │    chatTUI Client   │
    │   (Bubble Tea TUI)  │                     │   (Bubble Tea TUI)  │
    │                     │                     │                     │
    │ • INK-Style Auth    │                     │ • INK-Style Auth    │
    │ • Multi-Pane View   │                     │ • Multi-Pane View   │
    │ • Viewport Scroller │                     │ • Viewport Scroller │
    │ • Modal Overlays    │                     │ • Modal Overlays    │
    │ • Auto-Reconnect    │                     │ • Auto-Reconnect    │
    └─────────────────────┘                     └─────────────────────┘
```

The client communicates exclusively with the server via the custom TCP protocol. The server is strictly authoritative for permissions, messages, authentication, presence, and read states.

---

## Data Model & Persistence

chatTUI uses SQLite in WAL mode with relational tables and indexes:

- `users`: `id`, `username` (UNIQUE), `display_name`, `password_hash`, `user_color`, `custom_status`, `presence_state`, `last_seen_at`, `created_at`
- `sessions`: `token` (PK), `user_id` (FK), `created_at`, `expires_at`
- `rooms`: `id`, `name` (UNIQUE), `description`, `is_private`, `password_hash`, `owner_id` (FK), `is_temporary`, `expires_at`, `created_at`
- `room_members`: `room_id` (FK), `user_id` (FK), `role` (`owner`/`admin`/`moderator`/`member`), `joined_at`
- `room_bans`: `room_id`, `user_id`, `banned_by`, `reason`, `created_at`
- `room_mutes`: `room_id`, `user_id`, `muted_by`, `expires_at`, `created_at`
- `messages`: `id`, `sender_id`, `target_type` (`room`/`dm`), `room_id`, `recipient_id`, `content`, `created_at`, `is_system`
- `read_states`: `user_id`, `target_type`, `target_id`, `last_read_message_id`, `updated_at`
- `notifications`: `id`, `user_id`, `type`, `title`, `body`, `is_read`, `created_at`

Indexes exist on `username`, message `created_at`, room messages, DMs, member lists, and unread states.

---

## Quickstart Guide

### Prerequisites

- [Go 1.22+](https://golang.org/dl/)

### Building from Source

```bash
git clone https://github.com/rtoms/chattui.git
cd chattui

# Build both server and client binaries
go build -o chattui-server ./cmd/server
go build -o chattui-client ./cmd/client
```

### Starting the Server

```bash
# Run server on default port 8443 with database chattui.db
./chattui-server

# Or customize flags:
./chattui-server -port 8443 -db /path/to/chattui.db -retention-check 15 -expiry-check 1
```

### Launching the Client

Open a terminal window:
```bash
./chattui-client -server 127.0.0.1:8443
```

On first launch:
1. Fill in **Username**, **Display Name**, **Password**, and select your **User Color**.
2. Press **Enter** to create your account.
3. You will immediately be connected to `# global` and see:
   `● <username> just joined chatTUI`

---

## Deploying with Tailscale on Ubuntu

chatTUI is designed to operate seamlessly across private networks such as Tailscale.

### 1. Install Tailscale on the Server
```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
tailscale ip -4
# Example output: 100.101.102.103
```

### 2. Run chatTUI Server as a Systemd Service
Create `/etc/systemd/system/chattui.service`:
```ini
[Unit]
Description=chatTUI Server
After=network.target tailscaled.service

[Service]
Type=simple
User=ubuntu
WorkingDirectory=/home/ubuntu/chattui
ExecStart=/home/ubuntu/chattui/chattui-server -port 8443 -db /home/ubuntu/chattui/chattui.db
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start the service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now chattui
```

### 3. Connect from Any Tailscale Machine
On your laptop or workstation (connected to the same Tailnet):
```bash
chattui-client -server 100.101.102.103:8443
```

---

## Docker Deployment

Use Docker Compose for an isolated container deployment:

```bash
docker compose up -d --build
```

Logs can be viewed with:
```bash
docker compose logs -f chattui-server
```

---

## Keyboard Shortcuts & Commands

### Keyboard Shortcuts

| Shortcut | Action |
| :--- | :--- |
| `Ctrl + K` | Open Command / Search Palette |
| `Ctrl + N` | Start Direct Message (DM) |
| `Ctrl + R` | Open Room Browser / Create Room |
| `Ctrl + P` | View & Edit Profile |
| `Ctrl + H` | Display Help Modal |
| `Ctrl + Q` | Clean Disconnect & Quit |
| `Alt + ↑ / ↓` | Navigate between Sidebar Channels |
| `Page Up / Down` | Scroll message viewport up/down |
| `Home` / `End` | Jump to top / Jump to newest message |
| `Esc` | Close any active modal or dialog |
| `Tab` | Switch focus / toggle forms |

### Slash Commands

| Command | Description |
| :--- | :--- |
| `/help` | Show list of all available commands |
| `/who` | List all users and active presence |
| `/msg <user> <message>` | Send a direct message |
| `/room list` | Open public room browser |
| `/room create <name>` | Create a new room |
| `/room join <name> [password]` | Join an existing room |
| `/room leave` | Leave the current room |
| `/color <Cyan\|Blue\|...>` | Update your user display color |
| `/status <status text>` | Set your personal custom status |
| `/profile` | Open profile screen |
| `/kick <user>` | *(Moderator+)* Kick a user from current room |
| `/ban <user> [reason]` | *(Admin+)* Ban a user from current room |
| `/mute <user> [minutes]` | *(Moderator+)* Mute a user for *N* minutes |
| `/transfer <user>` | *(Owner)* Transfer room ownership to member |
| `/quit` | Cleanly disconnect and exit |

---

## Room Permissions Matrix

Server-side permission enforcement follows a strict 4-level hierarchy:

| Permission | Owner | Admin | Moderator | Member |
| :--- | :---: | :---: | :---: | :---: |
| Send / Read Messages | ✅ | ✅ | ✅ | ✅ |
| Leave Room | Transfer First | ✅ | ✅ | ✅ |
| Kick Lower Roles | ✅ | ✅ | ✅ | ❌ |
| Mute Lower Roles | ✅ | ✅ | ✅ | ❌ |
| Ban Lower Roles | ✅ | ✅ | ❌ | ❌ |
| Manage Moderators | ✅ | ✅ | ❌ | ❌ |
| Manage Admins | ✅ | ❌ | ❌ | ❌ |
| Transfer Ownership | ✅ | ❌ | ❌ | ❌ |
| Delete Room | ✅ | ❌ | ❌ | ❌ |

---

## Development & Automated Testing

### Run All Unit & Integration Tests

```bash
# Run all tests with Go's race condition detector
go test -v -race ./internal/...
```

Test coverage includes:
- Account creation & duplicate username rejection
- Password hashing and login verification
- Room creation, password protection, and public/private visibility
- Role permissions hierarchy (Owner > Admin > Mod > Member)
- Moderation restrictions (kick, ban, mute)
- Room ownership transfer
- Temporary room expiration worker
- 24-hour message retention cleanup query
- `@username` mention parsing & notification dispatch
- Presence tracking, idle detection, and live events
- End-to-end multi-client TCP networking

---

## License

MIT License. Built with ❤️ in Go for terminal enthusiasts.
