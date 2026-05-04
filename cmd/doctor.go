package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/plainwork/boxx/engine/bootstrap"
	"github.com/plainwork/boxx/engine/dockerx"
	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that this host is ready to run boxx",
	RunE: func(c *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		host := bootstrap.Detect()
		printf("OS:           %s/%s (%s)\n", host.OS, host.Arch, host.Distro)

		if err := dockerx.Ping(ctx); err != nil {
			printBad("Docker:       not reachable (%v)", err)
			printf("              Run: boxx bootstrap   to install Docker (Linux/Pi only)\n")
		} else {
			ver, _ := dockerx.ServerVersion(ctx)
			printOK("Docker:       reachable (%s)", ver)
		}

		for _, p := range []int{80, 443} {
			if bootstrap.PortFree(p) {
				printOK("Port %d:      free", p)
			} else {
				printBad("Port %d:      in use", p)
			}
		}

		if err := state.EnsureDirs(); err != nil {
			printBad("State dir:    %v", err)
		} else {
			printOK("State dir:    %s", state.Root())
		}

		free, total, err := bootstrap.DiskUsage(state.Root())
		if err == nil {
			printf("Disk (state): %s free / %s total\n", human(free), human(total))
		}

		mem, err := bootstrap.MemTotal()
		if err == nil {
			printf("Memory:       %s total\n", human(mem))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func printf(f string, a ...any)    { fmt.Fprintf(os.Stdout, f, a...) }
func printOK(f string, a ...any)   { fmt.Fprintf(os.Stdout, "  ok   "+f+"\n", a...) }
func printBad(f string, a ...any)  { fmt.Fprintf(os.Stdout, "  FAIL "+f+"\n", a...) }

func human(b uint64) string {
	const u = 1024
	if b < u {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(u), 0
	for n := b / u; n >= u; n /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
