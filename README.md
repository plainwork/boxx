# boxx

A tiny TUI + CLI for installing and orchestrating dockerized apps on a single Linux host.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/plainwork/boxx/main/install.sh | sh
```

On Linux this will also install Docker (if missing), enable it as a systemd service so it
starts on reboot, and add your user to the `docker` group. After the script finishes, run:

```sh
boxx install <image> --host <hostname>   # deploy your first app
boxx upgrade                             # upgrade boxx to the latest release
boxx                                     # open the TUI
```

## The contract

An image works with boxx if it:

1. Is a Docker image (public or private registry)
2. Listens HTTP on port 80
3. Exposes `GET /up` returning 2xx
4. Persists data under `/storage`
5. Reads its DB connection from `DATABASE_URL` (when a DB is requested)

## Hostnames

Every app has one primary hostname and any number of extra ones. Each hostname
gets its own Let's Encrypt certificate; point its DNS at the server first.

```sh
# serve on several hostnames; the first --host is the primary
boxx install <image> --host example.com --host app.example.net

# www.example.com 308-redirects to example.com (path and query kept)
boxx install <image> --host example.com --redirect-host www.example.com

# change them later (or use "hostnames" in the TUI app menu)
boxx host ls <app>
boxx host add <app> www.example.com --redirect
boxx host rm <app> app.example.net
boxx host primary <app> www.example.com
boxx host redirect <app> www.example.com --off
```

For groups, use the group slug: hostnames belong to the whole group.

A hostname can belong to only one app; boxx refuses duplicates, and
`boxx doctor` reports any left over from older versions.

**Subdomains into one app.** Add a wildcard and read the `Host` header in your
app to tell tenants apart:

```sh
boxx host add <app> '*.example.com'
```

- Create a wildcard DNS record (`*.example.com` → server IP).
- `*` matches exactly one label: `a.example.com`, not `example.com` or `a.b.example.com`.
- Each subdomain gets its own certificate on its first HTTPS visit, which takes a
  few seconds. Only subdomains matching one of your wildcards are issued.
- Let's Encrypt allows about 50 new certificates per week per registered domain.
- An exact hostname on another app (e.g. `api.example.com`) wins over the wildcard.
- Needs Caddy v2.8+ in the proxy; `boxx doctor` checks this.

## Upgrading

See [docs/upgrading.md](docs/upgrading.md).

## Dev

```sh
make build
./boxx doctor   # check the host
./boxx          # launch the TUI
make release VERSION=v0.1.0   # tag and push to trigger a GitHub release
```
