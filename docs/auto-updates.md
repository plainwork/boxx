# Linux Server: Automatic OS Updates (Ubuntu / Debian)

## 1. Install and enable

```sh
apt install -y unattended-upgrades
systemctl enable --now unattended-upgrades
```

## 2. Apply pending updates now

```sh
apt update && apt upgrade -y
apt autoremove -y    # remove unused dependencies
apt autoclean        # clear out stale package cache
```

## 3. Configure behaviour

Edit `/etc/apt/apt.conf.d/50unattended-upgrades` — key settings:

```
// Security + ESM security updates only (default). Uncomment to also get regular updates:
// "${distro_id}:${distro_codename}-updates";

Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";

// Auto-reboot when required (e.g. kernel updates). Reboots at 4am if needed:
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:00";
```

Ensure your Docker containers use `restart: unless-stopped` so they come back up after a reboot (boxx handles this via caddy/docker).

## 4. Configure frequency

Edit `/etc/apt/apt.conf.d/20auto-upgrades`:

```
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
```

`"1"` = daily. Both files must be present for automatic upgrades to run.

## 5. Verify

```sh
unattended-upgrade --dry-run -v
```

### Common warning: distro-info-data outdated

```
Could not figure out development release: Distribution data outdated.
Please check for an update for distro-info-data.
```

Harmless. Fix with:

```sh
apt update && apt install -y distro-info-data
```

Only matters for tools like `do-release-upgrade`, not for unattended-upgrades itself.

## 6. Ubuntu Pro / ESM Apps (optional)

ESM Apps covers packages like `ffmpeg`, `ansible`, etc. Free for up to 5 machines.

```sh
pro attach         # follow prompts — needs an Ubuntu One account
pro status         # verify esm-apps is enabled
```

Not required unless you're running packages from the `esm-apps` repo.

---

## Key paths

| Path | Purpose |
|------|---------|
| `/etc/apt/apt.conf.d/50unattended-upgrades` | Main config (what to upgrade, reboot, cleanup) |
| `/etc/apt/apt.conf.d/20auto-upgrades` | Schedule config (how often to run) |
| `/var/log/unattended-upgrades/` | Upgrade logs |
| `/var/run/reboot-required` | Exists when a reboot is pending |
