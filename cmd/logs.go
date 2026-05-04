package cmd

import (
	"os"
	"os/exec"

	"github.com/plainwork/boxx/engine/installer"
	"github.com/spf13/cobra"
)

var logsFollow bool
var logsTail int

var logsCmd = &cobra.Command{
	Use:   "logs <slug>",
	Short: "Tail logs for an installed app",
	Long:  `Tail docker logs for an installed app. Use "<group>/<app>" form for grouped apps.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		container, err := installer.LiveContainer(args[0])
		if err != nil {
			return err
		}
		dargs := []string{"logs", "--tail", itoa(logsTail)}
		if logsFollow {
			dargs = append(dargs, "-f")
		}
		dargs = append(dargs, container)
		cmd := exec.Command("docker", dargs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	},
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	logsCmd.Flags().IntVar(&logsTail, "tail", 100, "number of lines to show from the end")
	rootCmd.AddCommand(logsCmd)
}
