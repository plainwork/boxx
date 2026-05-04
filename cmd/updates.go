package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/plainwork/boxx/engine/state"
	"github.com/plainwork/boxx/engine/systemd"
	"github.com/plainwork/boxx/engine/updates"
	"github.com/spf13/cobra"
)

var (
	updatePolicyMode string
	updatePolicyApp  string
	updateCheckApp   string
)

var updatesCmd = &cobra.Command{
	Use:   "updates",
	Short: "Manage per-app update policies and check for new image versions",
}

// boxx updates check [slug [--app appSlug]]
var updatesCheckCmd = &cobra.Command{
	Use:   "check [slug]",
	Short: "Check for available image updates (no deploy)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		s, err := state.Load()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "APP\tIMAGE\tSTATUS\tNOTE")

		if len(args) == 0 {
			// Check all.
			results, err := updates.CheckAll(ctx, s, nil)
			if err != nil {
				return err
			}
			for _, r := range results {
				label := r.Slug
				if r.AppSlug != "" {
					label += "/" + r.AppSlug
				}
				printResult(w, label, r)
			}
		} else {
			slug := args[0]
			appSlug := updateCheckApp
			r, err := updates.Check(ctx, s, slug, appSlug)
			if err != nil {
				return err
			}
			label := slug
			if appSlug != "" {
				label += "/" + appSlug
			}
			printResult(w, label, r)
		}
		return w.Flush()
	},
}

func printResult(w *tabwriter.Writer, label string, r updates.Result) {
	status := "up-to-date"
	note := ""
	if r.Error != nil {
		status = "error"
		note = r.Error.Error()
	} else if r.UpdateAvailable {
		status = "update-available"
		if r.AvailableDigest != "" {
			note = r.AvailableDigest[:min(12, len(r.AvailableDigest))]
		}
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", label, r.Image, status, note)
}

// boxx updates run  (called by systemd timer)
var updatesRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Check and auto-deploy apps with update mode = auto (called by systemd timer)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		s, err := state.Load()
		if err != nil {
			return err
		}

		return updates.RunAuto(ctx, s, func(slug, msg string) {
			fmt.Printf("[%s] %s\n", slug, msg)
		})
	},
}

// boxx updates policy <slug> --mode off|notify|auto [--app appSlug]
var updatesPolicyCmd = &cobra.Command{
	Use:   "policy <slug>",
	Short: "Set update policy for an app (off, notify, auto)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := args[0]
		mode := state.UpdateMode(updatePolicyMode)
		switch mode {
		case state.UpdateModeOff, state.UpdateModeNotify, state.UpdateModeAuto:
		default:
			return fmt.Errorf("invalid mode %q — must be off, notify, or auto", updatePolicyMode)
		}

		s, err := state.Load()
		if err != nil {
			return err
		}

		appSlug := updatePolicyApp
		if appSlug == "" {
			app, ok := s.Singles[slug]
			if !ok {
				return fmt.Errorf("no single app %q (for grouped apps use --app)", slug)
			}
			app.UpdatePolicy.Mode = mode
			s.Singles[slug] = app
		} else {
			grp, ok := s.Groups[slug]
			if !ok {
				return fmt.Errorf("no group %q", slug)
			}
			app, ok := grp.Apps[appSlug]
			if !ok {
				return fmt.Errorf("group %q has no app %q", slug, appSlug)
			}
			app.UpdatePolicy.Mode = mode
			grp.Apps[appSlug] = app
			s.Groups[slug] = grp
		}

		if err := state.Save(s); err != nil {
			return err
		}
		fmt.Printf("update policy for %s set to %s\n", slug, mode)
		return nil
	},
}

// boxx updates timer install|remove|status
var updatesTimerCmd = &cobra.Command{
	Use:   "timer <install|remove|status>",
	Short: "Manage the systemd timer for automatic update checks",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "install":
			if err := systemd.Install(); err != nil {
				if err == systemd.ErrNotLinux {
					fmt.Println("systemd timer is only supported on Linux; run `boxx updates run` from cron instead.")
					return nil
				}
				return err
			}
			fmt.Println("boxx-updates timer installed and started")
		case "remove":
			if err := systemd.Remove(); err != nil {
				if err == systemd.ErrNotLinux {
					fmt.Println("systemd timer is only supported on Linux")
					return nil
				}
				return err
			}
			fmt.Println("boxx-updates timer removed")
		case "status":
			out, err := systemd.Status()
			if err != nil {
				if err == systemd.ErrNotLinux {
					fmt.Println("systemd timer is only supported on Linux")
					return nil
				}
				return err
			}
			fmt.Print(out)
		default:
			return fmt.Errorf("unknown subcommand %q — use install, remove, or status", args[0])
		}
		return nil
	},
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func init() {
	updatesCheckCmd.Flags().StringVar(&updateCheckApp, "app", "", "app slug (for grouped apps)")
	updatesPolicyCmd.Flags().StringVar(&updatePolicyMode, "mode", "", "update mode: off, notify, auto (required)")
	updatesPolicyCmd.Flags().StringVar(&updatePolicyApp, "app", "", "app slug (for grouped apps)")
	_ = updatesPolicyCmd.MarkFlagRequired("mode")

	updatesCmd.AddCommand(updatesCheckCmd, updatesRunCmd, updatesPolicyCmd, updatesTimerCmd)
	rootCmd.AddCommand(updatesCmd)
}
