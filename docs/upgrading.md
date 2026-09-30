# Upgrading boxx

## On each server

```sh
sudo boxx upgrade
```

This downloads the latest release, replaces the binary, and re-applies the
proxy config using the new binary. `sudo` is needed when boxx lives in
`/usr/local/bin`.

Alternatively, re-run the installer (optionally pinning a version):

```sh
curl -fsSL https://raw.githubusercontent.com/plainwork/boxx/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/plainwork/boxx/main/install.sh | VERSION=v0.5.0 sh
```

The installer doesn't touch the proxy, so follow it with:

```sh
sudo boxx proxy reload
```

Then check the host:

```sh
boxx doctor
```

### Upgrading from a version without multiple hostnames

- Existing state works as-is; no migration is needed.
- Releases before this one reloaded the proxy with the *old* binary during
  `boxx upgrade`. Run `sudo boxx proxy reload` once after upgrading so the new
  proxy config takes effect. Later upgrades do this automatically.
- Wildcard hostnames need Caddy v2.8+ in the proxy container. `boxx doctor`
  reports the version once you add a wildcard. To update Caddy:

  ```sh
  docker pull caddy:2
  docker rm -f boxx-proxy
  sudo boxx proxy start
  ```

  Sites are down for a few seconds. Certificates are kept in
  `/var/lib/boxx/caddy/data`, so nothing is re-issued.

### Downgrading

Older versions keep serving each app's primary hostname, but ignore extra
hostnames, and drop them from `state.json` the next time they save it.

## Releasing (maintainers)

```sh
make release VERSION=vX.Y.Z
```

This tags and pushes; GitHub Actions builds the release with goreleaser.
