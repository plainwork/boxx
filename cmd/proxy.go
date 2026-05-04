package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/plainwork/boxx/engine/caddy"
	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Manage the boxx Caddy proxy",
}

var proxyStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Caddy proxy container (idempotent)",
	RunE: func(c *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		s, err := state.Load()
		if err != nil {
			return err
		}
		if err := caddy.Ensure(ctx, s.Proxy.Image); err != nil {
			return err
		}
		if err := caddy.Apply(ctx, s); err != nil {
			return err
		}
		s.Proxy.Running = true
		if s.Proxy.Image == "" {
			s.Proxy.Image = "caddy:2"
		}
		if err := state.Save(s); err != nil {
			return err
		}
		fmt.Println("proxy started; admin API on", caddy.AdminAddr)
		return nil
	},
}

var proxyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the proxy is running",
	RunE: func(c *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		running, err := caddy.Status(ctx)
		if err != nil {
			return err
		}
		if running {
			fmt.Println("proxy: running")
		} else {
			fmt.Println("proxy: stopped")
		}
		return nil
	},
}

var proxyReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Re-apply Caddy config from current boxx state",
	RunE: func(c *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s, err := state.Load()
		if err != nil {
			return err
		}
		return caddy.Apply(ctx, s)
	},
}

func init() {
	proxyCmd.AddCommand(proxyStartCmd, proxyStatusCmd, proxyReloadCmd)
	rootCmd.AddCommand(proxyCmd)
}
