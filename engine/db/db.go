// Package db provisions database containers (MySQL or Postgres) for boxx-managed apps.
//
// Conventions:
//   - Container name:  boxx-db-<slug>
//   - Volume:          boxx_db_<slug>     mounted at the engine's data dir
//   - Network:         boxx_net (no published ports — internal only)
//   - Database name:   <slug> (sanitized: '-' -> '_')
//   - Username:        "app"
//   - Password:        randomly generated, returned in the DB record
//
// The single env var injected into apps is DATABASE_URL.
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
	"github.com/plainwork/boxx/engine/util"
)

// Network must match caddy.Network. We re-declare here to avoid an import cycle.
const Network = "boxx_net"

// Provision creates and starts a DB container if one with this slug doesn't already exist.
// If a state.DB already describes this slug, it is returned as-is (idempotent).
func Provision(ctx context.Context, slug, engine, version string, existing *state.DB) (*state.DB, error) {
	if existing != nil {
		return existing, nil
	}
	switch engine {
	case "mysql":
		return provisionMySQL(ctx, slug, version)
	case "postgres", "postgresql":
		return provisionPostgres(ctx, slug, version)
	default:
		return nil, fmt.Errorf("unsupported db engine %q (want mysql or postgres)", engine)
	}
}

// URL returns the DATABASE_URL string apps should use to connect.
func URL(d *state.DB) string {
	if d == nil {
		return ""
	}
	switch d.Engine {
	case "mysql":
		return fmt.Sprintf("mysql://%s:%s@%s:3306/%s", d.Username, d.Password, d.Container, d.Database)
	case "postgres", "postgresql":
		return fmt.Sprintf("postgres://%s:%s@%s:5432/%s", d.Username, d.Password, d.Container, d.Database)
	}
	return ""
}

func dbName(slug string) string { return strings.ReplaceAll(slug, "-", "_") }

func provisionMySQL(ctx context.Context, slug, version string) (*state.DB, error) {
	if version == "" {
		// Pin to 8.0 — supports mysql_native_password which plain-TCP clients
		// (Bun, node-mysql2) require without SSL. mysql:8.4 removed it.
		version = "8.0"
	}
	containerName := "boxx-db-" + slug

	// Idempotent: if the container is already running (e.g. from a partial previous
	// install), recover the credentials from its env vars rather than failing.
	running, _ := dockerx.ContainerRunning(ctx, containerName)
	if running {
		env, err := dockerx.ContainerEnv(ctx, containerName)
		if err != nil {
			return nil, fmt.Errorf("db container %s already exists but could not read its env: %w", containerName, err)
		}
		rootPw := env["MYSQL_ROOT_PASSWORD"]
		return &state.DB{
			Engine:       "mysql",
			Version:      version,
			Container:    containerName,
			Database:     env["MYSQL_DATABASE"],
			Username:     env["MYSQL_USER"],
			Password:     env["MYSQL_PASSWORD"],
			RootPassword: rootPw,
		}, nil
	}

	// Stopped container with same name? Remove it so we can recreate fresh.
	exists, _ := dockerx.ContainerExists(ctx, containerName)
	if exists {
		if err := dockerx.Rm(ctx, containerName); err != nil {
			return nil, err
		}
	}

	rootPw := util.RandomPassword()
	d := &state.DB{
		Engine:       "mysql",
		Version:      version,
		Container:    containerName,
		Database:     dbName(slug),
		Username:     "app",
		Password:     util.RandomPassword(),
		RootPassword: rootPw,
	}
	if err := dockerx.NetworkEnsure(ctx, Network); err != nil {
		return nil, err
	}
	if err := dockerx.Run(ctx, dockerx.RunOpts{
		Name:    d.Container,
		Image:   "mysql:" + version,
		Network: Network,
		Restart: "unless-stopped",
		Env: []string{
			"MYSQL_ROOT_PASSWORD=" + rootPw,
			"MYSQL_DATABASE=" + d.Database,
			"MYSQL_USER=" + d.Username,
			"MYSQL_PASSWORD=" + d.Password,
		},
		// --default-authentication-plugin is the correct way to set the auth
		// plugin in mysql:8.0. The MYSQL_AUTHENTICATION_PLUGIN env var is NOT
		// recognised by the official image and is silently ignored.
		Cmd: []string{"--default-authentication-plugin=mysql_native_password"},
		Volumes: map[string]string{
			"boxx_db_" + slug: "/var/lib/mysql",
		},
	}); err != nil {
		return nil, err
	}
	if err := waitMySQL(ctx, d.Container, d.Username, d.Password, 90*time.Second); err != nil {
		return nil, err
	}
	// Explicitly switch the app user to mysql_native_password. Even when the
	// server default is set correctly, MySQL may create the MYSQL_USER with
	// caching_sha2_password on some builds. This makes it deterministic.
	if err := alterNativePassword(ctx, d.Container, rootPw, d.Username, d.Password); err != nil {
		return nil, fmt.Errorf("alter user native_password: %w", err)
	}
	return d, nil
}

func provisionPostgres(ctx context.Context, slug, version string) (*state.DB, error) {
	if version == "" {
		version = "16"
	}
	containerName := "boxx-db-" + slug

	// Idempotent: recover credentials from a running container.
	running, _ := dockerx.ContainerRunning(ctx, containerName)
	if running {
		env, err := dockerx.ContainerEnv(ctx, containerName)
		if err != nil {
			return nil, fmt.Errorf("db container %s already exists but could not read its env: %w", containerName, err)
		}
		return &state.DB{
			Engine:    "postgres",
			Version:   version,
			Container: containerName,
			Database:  env["POSTGRES_DB"],
			Username:  env["POSTGRES_USER"],
			Password:  env["POSTGRES_PASSWORD"],
		}, nil
	}

	// Stopped container with same name? Remove it so we can recreate fresh.
	exists, _ := dockerx.ContainerExists(ctx, containerName)
	if exists {
		if err := dockerx.Rm(ctx, containerName); err != nil {
			return nil, err
		}
	}

	d := &state.DB{
		Engine:    "postgres",
		Version:   version,
		Container: containerName,
		Database:  dbName(slug),
		Username:  "app",
		Password:  util.RandomPassword(),
	}
	if err := dockerx.NetworkEnsure(ctx, Network); err != nil {
		return nil, err
	}
	if err := dockerx.Run(ctx, dockerx.RunOpts{
		Name:    d.Container,
		Image:   "postgres:" + version,
		Network: Network,
		Restart: "unless-stopped",
		Env: []string{
			"POSTGRES_USER=" + d.Username,
			"POSTGRES_PASSWORD=" + d.Password,
			"POSTGRES_DB=" + d.Database,
		},
		Volumes: map[string]string{
			"boxx_db_" + slug: "/var/lib/postgresql/data",
		},
	}); err != nil {
		return nil, err
	}
	if err := waitPostgres(ctx, d.Container, d.Username, 90*time.Second); err != nil {
		return nil, err
	}
	return d, nil
}

// alterNativePassword forces the app user to mysql_native_password so that
// plain-TCP clients (Bun, node-mysql2 without ssl) can authenticate.
func alterNativePassword(ctx context.Context, container, rootPw, user, pass string) error {
	sql := fmt.Sprintf(
		"ALTER USER '%s'@'%%' IDENTIFIED WITH mysql_native_password BY '%s'; FLUSH PRIVILEGES;",
		user, pass,
	)
	_, err := dockerx.Exec(ctx, container,
		"mysql", "-h", "127.0.0.1", "-u", "root", "-p"+rootPw, "-e", sql,
	)
	return err
}

func waitMySQL(ctx context.Context, container, user, pass string, max time.Duration) error {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		_, err := dockerx.Exec(ctx, container, "mysqladmin", "ping",
			"-h", "127.0.0.1", "-u", user, "-p"+pass, "--silent")
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return errors.New("mysql did not become ready in time")
}

func waitPostgres(ctx context.Context, container, user string, max time.Duration) error {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		_, err := dockerx.Exec(ctx, container, "pg_isready", "-U", user)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return errors.New("postgres did not become ready in time")
}

// ResetAppUser re-grants credentials for the app user. Safe to call any time.
//
// For MySQL: if root_password is not in state (pre-v0.2 installs) it is
// recovered automatically from the container's env via docker inspect, and
// then written back into the state record so future calls are fast.
func ResetAppUser(ctx context.Context, d *state.DB) error {
	switch d.Engine {
	case "mysql":
		rootPw := d.RootPassword
		if rootPw == "" {
			// Recover from container env — MySQL set MYSQL_ROOT_PASSWORD at
			// first start; Docker keeps it in the container config forever.
			env, err := dockerx.ContainerEnv(ctx, d.Container)
			if err != nil {
				return fmt.Errorf("cannot read container env to recover root password: %w", err)
			}
			rootPw = env["MYSQL_ROOT_PASSWORD"]
			if rootPw == "" {
				return errors.New("MYSQL_ROOT_PASSWORD not found in container env; cannot reset user")
			}
			// Persist it so we don't have to do this again.
			d.RootPassword = rootPw
		}
		// Use mysql_native_password explicitly so plain-TCP clients work.
		// Falls back to just IDENTIFIED BY if the plugin is unavailable (8.4+).
		sql := fmt.Sprintf(
			"ALTER USER '%s'@'%%' IDENTIFIED WITH mysql_native_password BY '%s'; GRANT ALL ON `%s`.* TO '%s'@'%%'; FLUSH PRIVILEGES;",
			d.Username, d.Password, d.Database, d.Username,
		)
		_, err := dockerx.Exec(ctx, d.Container,
			"mysql", "-uroot", "-p"+rootPw, "-e", sql)
		if err != nil {
			// mysql:8.4 removed mysql_native_password — retry without plugin specifier.
			sql2 := fmt.Sprintf(
				"ALTER USER '%s'@'%%' IDENTIFIED BY '%s'; GRANT ALL ON `%s`.* TO '%s'@'%%'; FLUSH PRIVILEGES;",
				d.Username, d.Password, d.Database, d.Username,
			)
			_, err = dockerx.Exec(ctx, d.Container,
				"mysql", "-uroot", "-p"+rootPw, "-e", sql2)
		}
		return err
	case "postgres", "postgresql":
		sql := fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s';", d.Username, d.Password)
		_, err := dockerx.Exec(ctx, d.Container,
			"psql", "-U", d.Username, "-d", d.Database, "-c", sql)
		return err
	default:
		return fmt.Errorf("unsupported engine %q", d.Engine)
	}
}
