package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/plainwork/boxx/engine/installer"
	"github.com/plainwork/boxx/engine/opslog"
	"github.com/spf13/cobra"
)

var logsFollow bool
var logsTail int
var logsBoxx bool

var logsCmd = &cobra.Command{
	Use:   "logs [slug]",
	Short: "Tail logs for an installed app, or boxx operational logs",
	Long: `Tail docker logs for an installed app. Use "<group>/<app>" form for grouped apps.

To view boxx's own operational log (installs, deploys, updates):
  boxx logs --boxx`,
	RunE: func(c *cobra.Command, args []string) error {
		if logsBoxx {
			return runBoxxLogs()
		}
		if len(args) == 0 {
			return fmt.Errorf("slug is required (or use --boxx to view boxx operational logs)")
		}
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

func runBoxxLogs() error {
	if logsFollow {
		// Stream via tail -f on the JSONL log file.
		logPath := opslog.LogFile()
		// Print existing tail first.
		events, _ := opslog.Tail(logsTail)
		for _, e := range events {
			printOpsEvent(e)
		}
		// Then follow.
		cmd := exec.Command("tail", "-f", "-n", "0", logPath)
		cmd.Stdout = os.NewFile(0, "") // discard raw — we decode below via pipe
		// Use tail -f and decode each line.
		pr, pw, err := os.Pipe()
		if err != nil {
			return err
		}
		cmd.Stdout = pw
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		dec := json.NewDecoder(pr)
		for {
			var e opslog.Event
			if err := dec.Decode(&e); err != nil {
				break
			}
			printOpsEvent(e)
		}
		return cmd.Wait()
	}

	events, err := opslog.Tail(logsTail)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Println("(no boxx operational log entries yet)")
		return nil
	}
	for _, e := range events {
		printOpsEvent(e)
	}
	return nil
}

func printOpsEvent(e opslog.Event) {
	ts := e.Time.Format("2006-01-02 15:04:05")
	slug := e.Slug
	if e.AppSlug != "" {
		slug += "/" + e.AppSlug
	}
	status := e.Status
	if e.Error != "" {
		status = "error: " + e.Error
	}
	dur := ""
	if e.DurationMS > 0 {
		dur = fmt.Sprintf(" (%dms)", e.DurationMS)
	}
	fmt.Printf("%s  %-14s  %-30s  %s%s\n", ts, e.Op, slug, status, dur)
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
	logsCmd.Flags().BoolVar(&logsBoxx, "boxx", false, "show boxx operational logs instead of app logs")
	rootCmd.AddCommand(logsCmd)
}
