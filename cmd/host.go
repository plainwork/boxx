package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/plainwork/boxx/engine/caddy"
	"github.com/plainwork/boxx/engine/hostnames"
	"github.com/plainwork/boxx/engine/installer"
	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

var (
	hostAddRedirect bool
	hostRedirectOff bool
)

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Manage the hostnames an app answers to",
	Long: `Manage the hostnames an app (or group) answers to.

Every app has one primary hostname plus any number of extra hostnames.
Extra hostnames either serve the app too, or 308-redirect to the primary.
A wildcard like *.example.com serves every subdomain one level deep; the
app can read the Host header to tell them apart.

Examples:
  boxx host add blog www.example.com --redirect
  boxx host add saas '*.example.com'
  boxx host primary blog example.com`,
}

var hostLsCmd = &cobra.Command{
	Use:   "ls <app>",
	Short: "List an app's hostnames",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		primary, aliases, err := installer.Hosts(args[0])
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "HOSTNAME\tROLE")
		fmt.Fprintf(w, "%s\tprimary\n", primary)
		for _, h := range aliases {
			role := "serves"
			if h.Redirect {
				role = "redirects to " + primary
			}
			if hostnames.IsWildcard(h.Name) {
				role += " (wildcard)"
			}
			fmt.Fprintf(w, "%s\t%s\n", h.Name, role)
		}
		return w.Flush()
	},
}

var hostAddCmd = &cobra.Command{
	Use:   "add <app> <hostname>...",
	Short: "Add hostnames to an app",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		add := []state.Host{}
		for _, h := range args[1:] {
			add = append(add, state.Host{Name: h, Redirect: hostAddRedirect})
		}
		return runHostEdit(args[0], func(ctx context.Context) error {
			return installer.AddHosts(ctx, args[0], add)
		}, args[1:])
	},
}

var hostRmCmd = &cobra.Command{
	Use:   "rm <app> <hostname>",
	Short: "Remove a hostname from an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		return runHostEdit(args[0], func(ctx context.Context) error {
			return installer.RemoveHost(ctx, args[0], args[1])
		}, nil)
	},
}

var hostPrimaryCmd = &cobra.Command{
	Use:   "primary <app> <hostname>",
	Short: "Make one of an app's hostnames the primary",
	Args:  cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		return runHostEdit(args[0], func(ctx context.Context) error {
			return installer.SetPrimary(ctx, args[0], args[1])
		}, nil)
	},
}

var hostRedirectCmd = &cobra.Command{
	Use:   "redirect <app> <hostname>",
	Short: "Make a hostname redirect to the primary (--off to serve the app again)",
	Args:  cobra.ExactArgs(2),
	RunE: func(c *cobra.Command, args []string) error {
		return runHostEdit(args[0], func(ctx context.Context) error {
			return installer.SetRedirect(ctx, args[0], args[1], !hostRedirectOff)
		}, nil)
	},
}

// runHostEdit runs a hostname change, then prints the resulting hostnames
// and notes about any newly added ones.
func runHostEdit(ref string, edit func(context.Context) error, added []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := edit(ctx); err != nil {
		return err
	}
	if err := hostLsCmd.RunE(hostLsCmd, []string{ref}); err != nil {
		return err
	}
	printHostNotes(ref, added)
	return nil
}

// printHostNotes explains what newly added hostnames need: DNS for
// wildcards, and which other app wins where hostnames overlap.
func printHostNotes(ref string, added []string) {
	if len(added) == 0 {
		return
	}
	s, err := state.Load()
	if err != nil {
		return
	}
	self, err := installer.HostOwner(s, ref)
	if err != nil {
		return
	}
	names := []string{}
	for _, h := range added {
		n, err := hostnames.Normalize(h)
		if err != nil {
			continue
		}
		names = append(names, n)
		if hostnames.IsWildcard(n) && !caddy.IsLocalHostname(n) {
			fmt.Printf("\n  note: point DNS for %s at this server (A/AAAA record).\n", n)
			fmt.Println("        Each subdomain gets its own certificate on its first HTTPS visit, which")
			fmt.Println("        takes a few seconds. Let's Encrypt allows about 50 new certificates per")
			fmt.Println("        week per registered domain.")
		}
	}
	for _, n := range hostnames.Overlaps(s, names, self) {
		fmt.Println("  note: " + n)
	}
}

// hostsFromFlags turns --host and --redirect-host values into a primary
// hostname (the first --host) and extra hostnames.
func hostsFromFlags(hosts, redirects []string) (string, []state.Host, error) {
	if len(hosts) == 0 {
		return "", nil, fmt.Errorf("--host is required")
	}
	aliases := []state.Host{}
	for _, h := range hosts[1:] {
		aliases = append(aliases, state.Host{Name: h})
	}
	for _, h := range redirects {
		aliases = append(aliases, state.Host{Name: h, Redirect: true})
	}
	return hosts[0], aliases, nil
}

func init() {
	hostAddCmd.Flags().BoolVar(&hostAddRedirect, "redirect", false, "redirect these hostnames to the primary instead of serving the app")
	hostRedirectCmd.Flags().BoolVar(&hostRedirectOff, "off", false, "stop redirecting; serve the app on this hostname again")
	hostCmd.AddCommand(hostLsCmd, hostAddCmd, hostRmCmd, hostPrimaryCmd, hostRedirectCmd)
	rootCmd.AddCommand(hostCmd)
}
