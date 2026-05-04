package installer

import (
	"fmt"
	"strings"

	"github.com/plainwork/boxx/engine/state"
)

// LiveContainer returns the currently-live docker container name for an installed
// app, in either "<slug>" (single) or "<group>/<app>" (grouped) form.
func LiveContainer(slugRef string) (string, error) {
	s, err := state.Load()
	if err != nil {
		return "", err
	}
	if i := strings.Index(slugRef, "/"); i >= 0 {
		gslug, aslug := slugRef[:i], slugRef[i+1:]
		g, ok := s.Groups[gslug]
		if !ok {
			return "", fmt.Errorf("no group %q", gslug)
		}
		a, ok := g.Apps[aslug]
		if !ok {
			return "", fmt.Errorf("group %q has no app %q", gslug, aslug)
		}
		return caddyContainerName(gslug+"-"+aslug, a.LiveColor), nil
	}
	a, ok := s.Singles[slugRef]
	if !ok {
		return "", fmt.Errorf("no app with slug %q", slugRef)
	}
	return caddyContainerName(a.Slug, a.LiveColor), nil
}
