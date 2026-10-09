# chatTUI

chatTUI is a terminal-based chat client and server written in Go. It provides real-time messaging across public channels, password-protected rooms, and direct messages using a Bubble Tea interface and a custom TCP wire protocol.

## Overview

chatTUI consists of two primary binaries: a dedicated server (`chattui-server`) and a terminal client (`chattui-client`). The server manages user authentication, SQLite session persistence, room administration, presence monitoring, and automatic message pruning. The client is built with Charm's Bubble Tea and Lip Gloss libraries, rendering a multi-pane terminal interface with auto-scrolling message viewports, modal dialogs, and real-time event updates.

Communication between the client and server takes place over raw TCP using length-prefixed JSON frames, allowing the service to run over local networks or private overlay networks such as Tailscale.

## Features

- Terminal user interface built with Bubble Tea, Bubbles, and Lip Gloss with dynamic window resizing support.
- Channel organization supporting default global chat, public rooms, password-protected private rooms, and ephemeral rooms that expire automatically.
- Direct messaging with unread message counters and live presence indicators.
- User mentions (`@username`) with in-app highlight formatting and toast notifications.
- Four-tier room role hierarchy (Owner, Admin, Moderator, Member) supporting kicks, bans, mutes, and room ownership transfer.
- User presence tracking (Online, Idle, Offline) with automatic idle detection after 5 minutes of client inactivity.
- Automated background workers for 24-hour message retention cleanup and temporary room expiration.
- Zero-CGO SQLite persistence operating in WAL mode using the pure Go `modernc.org/sqlite` driver.
- Custom wire protocol enforcing a 1 MB maximum frame size over raw TCP connections.

## Tech Stack

- Language: Go
- TUI Framework: Charm Bubble Tea, Bubbles, Lip Gloss
- Database: SQLite (via `modernc.org/sqlite` in WAL mode)
- Authentication: `golang.org/x/crypto/bcrypt`
- Transport: TCP with 4-byte big-endian length-prefixed JSON payloads

## Requirements

- Go 1.24 or later (for compiling from source)
- Linux, macOS, or compatible Unix-like terminal environment
- Docker and Docker Compose (optional, for containerized server deployments)

## Installation

Clone the repository and compile the binaries:

```bash
git clone https://github.com/rtoms/chattui.git
cd chattui

# Build server and client binaries
go build -o chattui-server ./cmd/server
go build -o chattui-client ./cmd/client
```

## Usage

### Running the Server

Start the server using default settings (listens on `0.0.0.0:8443` and writes to `chattui.db` in the current directory):

```bash
./chattui-server
```

Server command-line flags:

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-port` | int | `8443` | TCP port to listen on |
| `-db` | string | `chattui.db` | File path for the SQLite database |
| `-retention-check` | int | `15` | Interval in minutes to purge messages older than 24 hours |
| `-expiry-check` | int | `1` | Interval in minutes to delete expired temporary rooms |

Example with custom configuration:

```bash
./chattui-server -port 9000 -db /var/lib/chattui/data.db -retention-check 30 -expiry-check 5
```

### Running the Client

Launch the client by providing the server address:

```bash
./chattui-client -server 127.0.0.1:8443
```

Client command-line flags:

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-server` | string | `127.0.0.1:8443` | Server address in `host:port` format |

### First Launch and Account Creation

When launching the client for the first time:

1. Select between Registration and Login modes.
2. In registration mode, enter a username (3 to 20 alphanumeric characters, underscores, or hyphens), display name, password, and optional user color and status message.
3. Upon registration, the server stores hashed credentials, establishes an authenticated session, and connects the client to `# global`.

## Deployment

### Tailscale Deployment on Linux

To host the server privately on a Tailscale network:

1. Install Tailscale and connect the server host to your tailnet:

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
tailscale ip -4
```

2. Create a systemd service file at `/etc/systemd/system/chattui.service`:

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

3. Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now chattui
```

4. Connect from another machine on the same tailnet:

```bash
./chattui-client -server <tailscale-ip>:8443
```

### Docker Deployment

Run the server container with Docker Compose:

```bash
docker compose up -d --build
```

View server logs:

```bash
docker compose logs -f chattui-server
```

The container exposes port `8443` and persists database storage in the `chattui-data` volume mapped to `/app/data`.

## Keyboard Shortcuts and Commands

### Keyboard Shortcuts

| Shortcut | Context | Action |
| --- | --- | --- |
| `Ctrl + K` | Chat view | Open command palette |
| `Ctrl + N` | Chat view | Open direct message user selector |
| `Ctrl + R` | Chat view | Open room browser |
| `Ctrl + P` | Chat view | Open profile settings modal |
| `Ctrl + H` | Chat view | Open help modal |
| `Ctrl + Q` | Global | Disconnect session and exit |
| `Alt + Up` / `Ctrl + Up` | Chat view | Select previous channel in sidebar |
| `Alt + Down` / `Ctrl + Down` | Chat view | Select next channel in sidebar |
| `Page Up` | Chat view | Scroll chat viewport up (pauses auto-scroll) |
| `Page Down` | Chat view | Scroll chat viewport down |
| `Home` | Chat view | Scroll to top of message history |
| `End` | Chat view | Scroll to latest message (resumes auto-scroll) |
| `Esc` | Modals / Chat view | Close modal, or toggle focus between input and sidebar |
| `Tab` | Chat view | Toggle focus between input and sidebar (when input is empty) |
| `Up` / `k` | Sidebar focused | Move channel selection up |
| `Down` / `j` | Sidebar focused | Move channel selection down |
| `Enter` | Sidebar focused | Activate selected channel and return focus to input |

### Slash Commands

Enter slash commands directly into the chat input field:

| Command | Arguments | Description |
| --- | --- | --- |
| `/help` | None | Open the help modal |
| `/who` | None | List users and their active presence status |
| `/msg` | `<user> <message>` | Send a direct message to a user |
| `/room list` | None | Open the public room browser |
| `/room create` | `<name>` | Create a new room (opens creation modal if name is omitted) |
| `/room join` | `<name> [password]` | Join an existing room with optional password |
| `/room leave` | None | Leave the active room (global chat cannot be left) |
| `/color` | `<color>` | Update display color (Cyan, Blue, Green, Yellow, Magenta, Red, White) |
| `/status` | `<status text>` | Update profile custom status string |
| `/profile` | None | Open profile editor modal |
| `/kick` | `<user>` | Kick a user from the current room (Moderator or higher) |
| `/ban` | `<user> [reason]` | Ban a user from the current room (Admin or higher) |
| `/mute` | `<user> [minutes]` | Mute a user for specified minutes (Moderator or higher, default 15) |
| `/transfer` | `<user>` | Open confirmation dialog to transfer room ownership (Owner only) |
| `/quit` | None | Disconnect and quit the application |

## Room Permissions Matrix

Server-side permission enforcement applies the following access levels:

| Action | Owner | Admin | Moderator | Member |
| --- | --- | --- | --- | --- |
| Read and Send Messages | Yes | Yes | Yes | Yes |
| Leave Room | Transfer First | Yes | Yes | Yes |
| Kick Lower Roles | Yes | Yes | Yes | No |
| Mute Lower Roles | Yes | Yes | Yes | No |
| Ban Lower Roles | Yes | Yes | No | No |
| Transfer Ownership | Yes | No | No | No |

Moderation actions cannot be performed on oneself or on users holding an equal or higher role.

## Wire Protocol

The client and server communicate over TCP using binary length-prefixed frames:

1. Frame Header: 4-byte unsigned big-endian integer indicating payload length.
2. Frame Body: JSON-encoded payload representing a `WireMessage` structure.
3. Size Limit: Maximum frame size is 1 MB; frames exceeding this size are rejected.

The server remains authoritative for authentication verification, message routing, permission enforcement, and room lifecycle events.

## Development and Testing

Run unit and integration test suites with Go's race detector:

```bash
go test -v -race ./internal/...
```

Test coverage includes:
- Authentication, credential hashing, and session validation
- Database initialization, user queries, and migration integrity
- Room lifecycle, permission checks, password validation, and ephemeral room expiration
- 24-hour message retention purging
- Mention extraction and notification dispatch
- Presence updates and idle state tracking
- Multi-client TCP networking

## License

This project is licensed under the MIT License.
