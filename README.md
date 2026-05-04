# boxx

A tiny TUI + CLI for installing and orchestrating dockerized apps on a single host
using ONCE-style conventions and Kamal-style zero-downtime deploys.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/plainwork/boxx/main/install.sh | sh
```

On Linux this will also install Docker (if missing), enable it as a systemd service so it
starts on reboot, and add your user to the `docker` group. After the script finishes, run:

```sh
boxx install <image> --host <hostname>   # deploy your first app
boxx                                     # open the TUI
```

## The contract

An image works with boxx if it:

1. Is a Docker image (public or private registry)
2. Listens HTTP on port 80
3. Exposes `GET /up` returning 2xx
4. Persists data under `/storage`
5. Reads its DB connection from `DATABASE_URL` (when a DB is requested)

## Dev

```sh
make build
./boxx doctor   # check the host
./boxx          # launch the TUI
make release VERSION=v0.1.0   # tag and push to trigger a GitHub release
```
