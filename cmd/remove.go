package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/plainwork/boxx/engine/caddy"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

var (
	rmKeepData bool
	rmYes      bool
)

var removeCmd = &cobra.Command{
	Use:   "remove <slug>",
	Short: "Remove an installed app or group",
	Long: `Stop and delete an installed single app or group.

For a single app slug:    removes that app, its DB (if any), volumes, and route.
For a group slug:         removes ALL apps in the group + the shared DB + volumes.
For "<group>/<app>":      removes just one app from the group; the shared DB stays.

By default, /storage and database volumes are deleted. Use --keep-data to keep them.`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		ref := args[0]
		s, err := state.Load()
		if err != nil {
			return err
		}

		// Decide what we're removing.
		var summary string
		switch {
		case strings.Contains(ref, "/"):
			gslug, aslug, _ := strings.Cut(ref, "/")
			g, ok := s.Groups[gslug]
			if !ok {
				return fmt.Errorf("no group %q", gslug)
			}
			if _, ok := g.Apps[aslug]; !ok {
				return fmt.Errorf("group %q has no app %q", gslug, aslug)
			}
			summary = fmt.Sprintf("remove app %q from group %q", aslug, gslug)
		default:
			if _, ok := s.Singles[ref]; ok {
				summary = "remove single app " + ref
			} else if _, ok := s.Groups[ref]; ok {
				summary = "remove ENTIRE group " + ref + " (all apps + shared DB)"
			} else {
				return fmt.Errorf("no app or group with slug %q", ref)
			}
		}

		if !rmYes && !confirm(summary+". Continue? [y/N]: ") {
			return fmt.Errorf("aborted")
		}

		switch {
		case strings.Contains(ref, "/"):
			gslug, aslug, _ := strings.Cut(ref, "/")
			return removeGroupApp(ctx, s, gslug, aslug)
		default:
			if _, ok := s.Singles[ref]; ok {
				return removeSingle(ctx, s, ref)
			}
			if _, ok := s.Groups[ref]; ok {
				return removeGroup(ctx, s, ref)
			}
			// Not in state — orphaned containers from a failed install.
			// Clean up by name pattern so the user can retry.
			return removeOrphaned(ctx, ref)
		}
	},
}

func removeSingle(ctx context.Context, s *state.State, slug string) error {
	app := s.Singles[slug]
	for _, color := range []string{"blue", "green"} {
		_ = dockerx.Rm(ctx, "boxx-app-"+slug+"-"+color)
	}
	if app.DB != nil {
		_ = dockerx.Rm(ctx, app.DB.Container)
	}
	if !rmKeepData {
		removeVolumes(ctx, "boxx_storage_"+slug, "boxx_db_"+slug)
	}
	delete(s.Singles, slug)
	if err := caddy.Apply(ctx, s); err != nil {
		return err
	}
	return state.Save(s)
}

func removeGroup(ctx context.Context, s *state.State, gslug string) error {
	g := s.Groups[gslug]
	for aslug := range g.Apps {
		fullSlug := gslug + "-" + aslug
		for _, color := range []string{"blue", "green"} {
			_ = dockerx.Rm(ctx, "boxx-app-"+fullSlug+"-"+color)
		}
		if !rmKeepData {
			removeVolumes(ctx, "boxx_storage_"+fullSlug)
		}
	}
	if g.DB != nil {
		_ = dockerx.Rm(ctx, g.DB.Container)
		if !rmKeepData {
			removeVolumes(ctx, "boxx_db_"+gslug)
		}
	}
	delete(s.Groups, gslug)
	if err := caddy.Apply(ctx, s); err != nil {
		return err
	}
	return state.Save(s)
}

func removeGroupApp(ctx context.Context, s *state.State, gslug, aslug string) error {
	g := s.Groups[gslug]
	fullSlug := gslug + "-" + aslug
	for _, color := range []string{"blue", "green"} {
		_ = dockerx.Rm(ctx, "boxx-app-"+fullSlug+"-"+color)
	}
	if !rmKeepData {
		removeVolumes(ctx, "boxx_storage_"+fullSlug)
	}
	delete(g.Apps, aslug)
	if len(g.Apps) == 0 {
		// Last app gone — also remove shared DB and the group itself.
		if g.DB != nil {
			_ = dockerx.Rm(ctx, g.DB.Container)
			if !rmKeepData {
				removeVolumes(ctx, "boxx_db_"+gslug)
			}
		}
		delete(s.Groups, gslug)
	} else {
		s.Groups[gslug] = g
	}
	if err := caddy.Apply(ctx, s); err != nil {
		return err
	}
	return state.Save(s)
}

// removeOrphaned cleans up containers and volumes for a slug that is not in
// state (e.g. left behind by a failed install that rolled back before saving).
func removeOrphaned(ctx context.Context, slug string) error {
	// Remove known container name patterns.
	for _, color := range []string{"blue", "green"} {
		_ = dockerx.Rm(ctx, "boxx-app-"+slug+"-"+color)
	}
	_ = dockerx.Rm(ctx, "boxx-db-"+slug)
	if !rmKeepData {
		removeVolumes(ctx, "boxx_storage_"+slug, "boxx_db_"+slug)
	}
	fmt.Printf("removed orphaned resources for %q (was not in state)\n", slug)
	return nil
}

func removeVolumes(ctx context.Context, names ...string) {
	for _, n := range names {
		_ = dockerx.RmVolume(ctx, n)
	}
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func init() {
	removeCmd.Flags().BoolVar(&rmKeepData, "keep-data", false, "keep /storage and database volumes")
	removeCmd.Flags().BoolVarP(&rmYes, "yes", "y", false, "skip confirmation prompt")
	rootCmd.AddCommand(removeCmd)
}
