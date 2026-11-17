# FlowPilot Product Overview and Installation Guide

Current version: `0.1.0.16`

中文版：[简体中文](README.zh-CN.md)

## 1. Product overview

FlowPilot is a lightweight, integrated ComfyUI deployment and management platform for GPU physical servers. It provides one web console for hardware, ComfyUI instances, images, models, downloads, backups, and audit records. It is designed for GPU workstations, AI inference servers, and trusted private management networks.

Starting with this release, **the FlowPilot management service runs as a native Go process under systemd rather than inside Docker**. ComfyUI instances continue to use Docker, preserving instance isolation, image provenance, GPU allocation, and existing data compatibility.

### Key capabilities

- **Hardware and compatibility checks**: per-GPU inventory, NVIDIA driver/CUDA, CPU, memory, temperature, unified memory, disk pressure, and remediation guidance.
- **ComfyUI lifecycle management**: create, start, stop, restart, rebuild, GPU/port assignment, isolated workspaces, live logs, and queue health.
- **Image center**: trusted image detection, pulls, builds pinned to immutable ComfyUI commits, import/export, tags, and build logs.
- **Model center**: shared repositories, ModelScope/Hugging Face search, URL downloads, local import, NAS sources, resumable downloads, and SHA256 verification.
- **Model bundles**: MiniMax H3 preflight, license acknowledgment, serial installation, pause/resume/cancel/retry, and instance linking.
- **Unified task center**: status, progress, and logs for downloads, imports, builds, pulls, migrations, bundles, and backups.
- **Data protection**: SQLite persistence, per-instance workspaces, platform backups, pre-upgrade snapshots, and health-gated rollback.
- **Security and audit**: single `admin` account, Argon2id password hashes, server-side sessions, CSRF/Origin checks, login throttling, and operation audit trails.

## 2. Architecture and scope

```text
Browser
  │ HTTP :18080 (default)
  ▼
flowpilotd (native Go process managed by systemd)
  ├── SQLite/models/workflows/outputs: /var/lib/flowpilot
  ├── Docker Unix socket: /var/run/docker.sock
  └── ComfyUI container instances (Docker with optional GPUs)
```

- Go, Node.js, Python, Nginx, and Docker Compose are not required on the target server.
- Docker Engine is still required for ComfyUI instances. The installer offers to install it if missing.
- The service listens on TCP `18080` on all interfaces by default. FlowPilot does not provide built-in TLS. Do not expose it directly to an untrusted public network; use a trusted management network or add your own HTTPS reverse proxy.
- Supported targets are Ubuntu/Debian with systemd on Linux `amd64` or `arm64`. NVIDIA GPUs are recommended. CPU-only management requires an explicit acknowledgment.

## 3. Prerequisites

1. An Ubuntu 22.04/24.04 or compatible Debian physical/virtual server with `root` or `sudo` access.
2. At least 1 GiB free disk space for FlowPilot; allow 20 GiB or more for ComfyUI images and models.
3. A working Docker Engine. If absent, approve `docker.io` installation from the configured system APT repository in the wizard, or use `--install-docker`.
4. GPU instances require a working NVIDIA driver, and GPU containers require NVIDIA Container Toolkit. The installer detects and reports these dependencies but never replaces a driver automatically.
5. Access to GitHub Releases, an offline release archive, or a suitable mirror. Use `--region cn` for mainland China network conditions.

## 4. Interactive online installation (recommended)

```bash
curl -fL --retry 3 \
  -o flowpilot-native.sh \
  https://github.com/ghoul02015/flowpilot/releases/download/v0.1.0.16/flowpilot-native.sh
chmod +x flowpilot-native.sh
sudo ./flowpilot-native.sh install
```

The wizard prompts for:

1. Web port, default `18080`;
2. Program directory, default `/opt/flowpilot`;
3. Data directory, default `/var/lib/flowpilot`;
4. Network policy: `auto`, `cn`, or `global`;
5. Whether to detach the installation into the background;
6. Docker, GPU, network, disk, port, and runtime preflight checks;
7. The initial `admin` password, entered without echo and confirmed. It must contain at least 8 characters, including letters and digits.

When complete, open:

```text
http://<server-address>:18080
Username: admin
Password: the password set during installation
```

The password is submitted through the local setup API and stored only as an Argon2id hash. It is not placed in the systemd environment, process arguments, or installer log.

## 5. Mainland China networks, proxies, and mirrors

### Automatic fallback

```bash
sudo ./flowpilot-native.sh install --region cn
```

- `auto`: try GitHub first, then built-in mirrors.
- `cn`: try built-in mirrors first, then GitHub.
- `global`: use GitHub and only an explicitly configured custom mirror.
- Built-in candidates are `ghfast.top`, `gh-proxy.com`, and `ghproxy.net`. Third-party availability changes over time; do not rely on any one mirror as the permanent distribution channel.

### Custom mirror or proxy

```bash
sudo ./flowpilot-native.sh install \
  --mirror https://ghfast.top/

# Standard proxy variables are supported. Logs record only whether a proxy is set.
export HTTPS_PROXY=http://proxy.example.com:8080
sudo -E ./flowpilot-native.sh install
```

An online archive is extracted only after it matches the release `SHA256SUMS`. This detects corruption and mismatch with the published manifest. Where possible, obtain the installer and checksum manifest from the official repository as well. Mirrors are transport channels only; never enter GitHub credentials on a mirror website.

Source dependency mirrors are also supported when building release archives:

```bash
FLOWPILOT_NETWORK_REGION=cn code/scripts/package-native-release.sh
# Or set FLOWPILOT_NPM_REGISTRY and FLOWPILOT_GOPROXY independently.
```

## 6. Background installation, disconnects, and resume

### Background installation

Select background mode in the wizard or run:

```bash
sudo ./flowpilot-native.sh install --background
```

The installer starts a detached session and records its worker PID and full log. Closing the SSH terminal does not stop the installation.

```bash
sudo ./flowpilot-native.sh status
sudo tail -f /var/log/flowpilot/install/latest.log
```

### Resume after interruption

If a foreground installation is interrupted by Ctrl+C, SSH disconnection, or a network failure, its `.part` downloads, configuration, and stage markers are retained. After fixing the cause, run:

```bash
sudo ./flowpilot-native.sh resume
```

Downloads use HTTP Range resume. Directory creation, service configuration, and health checks are idempotent. A lock prevents concurrent install/uninstall processes.

## 7. Unattended installation

### Supply a password file

```bash
umask 077
printf '%s\n' 'ReplaceWithAStrongPassword123' > /root/flowpilot-password
sudo ./flowpilot-native.sh install \
  --non-interactive \
  --background \
  --password-file /root/flowpilot-password \
  --allow-no-gpu
shred -u /root/flowpilot-password 2>/dev/null || rm -f /root/flowpilot-password
```

The password file must have mode `0600`. `--allow-no-gpu` is needed only when no working NVIDIA driver is available; omit it on a healthy GPU server.

### Generate a random initial password

```bash
sudo ./flowpilot-native.sh install \
  --non-interactive --background --random-password --allow-no-gpu
sudo cat /var/lib/flowpilot/initial-credentials
```

The generated credential file is readable only by root (`0600`). Change the password after the first login and remove the initial credential file securely.

Common unattended options:

```text
--port 18080
--region cn
--mirror https://ghfast.top/
--install-docker
--background
--allow-no-gpu
```

## 8. Offline installation

Download the archive for your architecture and `SHA256SUMS` from the same release:

```text
flowpilot-0.1.0.16-linux-amd64.tar.gz
flowpilot-0.1.0.16-linux-arm64.tar.gz
SHA256SUMS
```

On the target server:

```bash
sha256sum -c --ignore-missing SHA256SUMS
mkdir -p /tmp/flowpilot-native
tar -xzf flowpilot-0.1.0.16-linux-arm64.tar.gz \
  -C /tmp/flowpilot-native --strip-components=1
sudo /tmp/flowpilot-native/install.sh install
```

Substitute the `amd64` archive where appropriate. The offline archive contains the Go service, frontend, installer, documentation, and license files; GitHub access is not required on the target. Docker or NVIDIA installation and later ComfyUI image/model downloads may still require network access.

For a custom data directory under a systemd-protected path such as `/home`, the installer automatically uses `ProtectHome=read-only` and grants the `flowpilot` account traverse-only access (`--x`) to parent directories, without permission to list their contents. `ReadWritePaths` remains limited to the data and log directories, and the original ACLs are restored during uninstall. This requires the `acl` package (`setfacl` / `getfacl`); the installer attempts an `apt` installation when it is missing, so fully offline hosts should preinstall it. The default `/var/lib/flowpilot` layout retains the stricter `ProtectHome=true` setting.

## 9. Installation layout

| Path | Purpose |
|---|---|
| `/opt/flowpilot/releases/0.1.0.16/` | Versioned native service and frontend |
| `/opt/flowpilot/current` | Active release symlink for upgrades and rollback |
| `/opt/flowpilot/bin/flowpilot-native` | Unified install/resume/status/uninstall entry point |
| `/usr/local/bin/flowpilot-native` | Global command symlink |
| `/etc/systemd/system/flowpilot.service` | Native systemd unit |
| `/etc/flowpilot/flowpilot.env` | Non-secret runtime configuration |
| `/etc/flowpilot/installer.conf` | Resume configuration; contains no password |
| `/var/lib/flowpilot/flowpilot.db` | SQLite database |
| `/var/lib/flowpilot/models/` | Shared model repository |
| `/var/lib/flowpilot/instances/` | Isolated instance workspaces |
| `/var/lib/flowpilot/import/` | Allowed local model import directory |
| `/var/lib/flowpilot/backups/` | Platform and pre-upgrade backups |
| `/var/lib/flowpilot/.install/` | Partial downloads, stage markers, and short-lived setup files |
| `/var/log/flowpilot/install/` | Full install/uninstall logs |
| `/var/log/flowpilot/service.log` | Native service stdout and stderr |

## 10. Service management and logs

```bash
sudo systemctl status flowpilot
sudo systemctl restart flowpilot
sudo systemctl stop flowpilot
sudo systemctl enable flowpilot

sudo tail -f /var/log/flowpilot/service.log
sudo journalctl -u flowpilot -n 200 --no-pager
sudo tail -f /var/log/flowpilot/install/latest.log
```

Health check:

```bash
curl http://127.0.0.1:18080/api/v1/health
```

Installer logs include stages, download routes, preflight results, service state, and error locations. Passwords, setup tokens, and proxy URLs are not intentionally logged. Never place a password directly in a command-line argument.

## 11. Upgrade and rollback

Run `install` with the newer installer. It will:

1. Retain the previous versioned release directory;
2. Stop the service and copy SQLite plus WAL/SHM files into `backups/`;
3. Switch the `current` symlink atomically;
4. Run a health gate and, on failure, restore the previous symlink and attempt to restart the old service.

A database upgrade may apply forward migrations. A complete rollback may therefore require restoring the matching pre-upgrade database while the service is stopped. Preserve the consistency of the main database and its `-wal`/`-shm` files.

## 12. Uninstall and complete cleanup

### Default uninstall: preserve business data

```bash
sudo flowpilot-native uninstall
# Non-interactive:
sudo flowpilot-native uninstall --yes
```

This removes the native program, systemd service, and download cache. It preserves `/var/lib/flowpilot`, logs, Docker, NVIDIA drivers, ComfyUI containers, and images.

### Permanently remove platform data

```bash
sudo flowpilot-native uninstall --purge
```

This additionally deletes models, workflows, outputs, SQLite, backups, configuration, and logs. It is irreversible. The wizard requires `yes`; automation must explicitly pass `--yes`.

### Also remove FlowPilot-managed ComfyUI containers

```bash
sudo flowpilot-native uninstall --purge --remove-instances
```

Only containers labeled `flowpilot.instance=true` are affected. Docker images and system packages remain installed to avoid disrupting other workloads.

## 13. Troubleshooting

### GitHub is unreachable

Use `--region cn`, `--mirror`, a standard HTTPS proxy, or an offline archive. If every route fails, check DNS, system time, CA certificates, and outbound firewall rules, then run `resume`.

### The port is occupied

Choose another port:

```bash
sudo ./flowpilot-native.sh install --port 28080
```

For remote access, allow the corresponding TCP port through the firewall, for example `sudo ufw allow 18080/tcp`.

### The NVIDIA driver works but GPU containers do not

Check `nvidia-smi` and `docker info | grep -i nvidia`. The installer does not modify the driver or NVIDIA Container Toolkit automatically, avoiding disruption to other physical-server workloads.

### The service does not start

Inspect `/var/log/flowpilot/service.log` and `journalctl -u flowpilot`. Check the Docker socket, `/var/lib/flowpilot` permissions, selected port, and the `flowpilot` user's Docker socket group membership.

### Forgotten password

After signing in, change the password in security settings. If access is lost, follow your organization's secure database recovery process. Never post a live database, setup token, or password to a public issue.

## 14. Open-source license

FlowPilot is licensed under **Apache License 2.0**, which permits use, modification, and distribution and includes an express patent grant and NOTICE preservation requirements. Retain `LICENSE`, `NOTICE`, and applicable third-party notices when redistributing. Models and workflows may have separate licenses; FlowPilot's Apache-2.0 license does not change their terms.

Repository: <https://github.com/ghoul02015/flowpilot>
