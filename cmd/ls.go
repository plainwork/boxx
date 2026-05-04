package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/plainwork/boxx/engine/state"
	"github.com/spf13/cobra"
)

var lsJSON bool

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List installed apps and groups",
	RunE: func(c *cobra.Command, args []string) error {
		s, err := state.Load()
		if err != nil {
			return err
		}
		if lsJSON {
			return json.NewEncoder(os.Stdout).Encode(s)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tSLUG\tHOSTNAME\tPATH\tIMAGE\tLIVE\tDB")

		ssl := keys(s.Singles)
		sort.Strings(ssl)
		for _, k := range ssl {
			a := s.Singles[k]
			fmt.Fprintf(w, "single\t%s\t%s\t/\t%s\t%s\t%s\n",
				a.Slug, a.Hostname, a.Image, a.LiveColor, dbLabel(a.DB))
		}

		gks := keys(s.Groups)
		sort.Strings(gks)
		for _, gk := range gks {
			g := s.Groups[gk]
			aks := keys(g.Apps)
			sort.Strings(aks)
			for _, ak := range aks {
				a := g.Apps[ak]
				fmt.Fprintf(w, "group\t%s/%s\t%s\t%s\t%s\t%s\t%s\n",
					g.Slug, a.Slug, g.Hostname, a.Path, a.Image, a.LiveColor, dbLabel(g.DB))
			}
		}
		return w.Flush()
	},
}

func dbLabel(d *state.DB) string {
	if d == nil {
		return "-"
	}
	return d.Engine + ":" + d.Version
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func init() {
	lsCmd.Flags().BoolVar(&lsJSON, "json", false, "output the full state document as JSON")
	rootCmd.AddCommand(lsCmd)
}
