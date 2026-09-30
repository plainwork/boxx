package envfile

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Managed keys are injected by boxx at deploy time and are never stored as
// app env: a stored value would go stale and override boxx's own.
var Managed = map[string]bool{
	"DATABASE_URL": true,
	"BASE_PATH":    true,
	"PORT":         true,
}

// StripManaged removes the managed keys from env.
func StripManaged(env map[string]string) {
	for k := range Managed {
		delete(env, k)
	}
}

// fallbackEditors are tried in order when neither $EDITOR nor $VISUAL is set.
var fallbackEditors = []string{"nano", "vi"}

// Editor returns the editor command (program plus any arguments, so
// EDITOR="code --wait" works) and where it came from: "$EDITOR", "$VISUAL",
// or "fallback" when neither is set and an installed nano or vi was found.
func Editor() (argv []string, source string, err error) {
	for _, v := range []string{"EDITOR", "VISUAL"} {
		if f := strings.Fields(os.Getenv(v)); len(f) > 0 {
			if _, err := exec.LookPath(f[0]); err != nil {
				return nil, "$" + v, fmt.Errorf("$%s is %q, which is not installed", v, f[0])
			}
			return f, "$" + v, nil
		}
	}
	for _, e := range fallbackEditors {
		if _, err := exec.LookPath(e); err == nil {
			return []string{e}, "fallback", nil
		}
	}
	return nil, "", errors.New("no editor found: set $EDITOR or install nano")
}

// WriteTemp writes env to a new temp file headed with editing instructions and
// returns its path. The caller removes it.
func WriteTemp(env map[string]string, label string) (string, error) {
	tmp, err := os.CreateTemp("", "boxx-env-*.env")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	fmt.Fprintf(tmp, "# boxx env — %s\n", label)
	fmt.Fprintf(tmp, "# Edit values below, then save and close to apply.\n")
	fmt.Fprintf(tmp, "# Lines starting with # are ignored. DATABASE_URL, BASE_PATH and PORT are managed by boxx.\n\n")
	fmt.Fprint(tmp, Format(env))
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// EditCmd returns the command that opens path in the user's editor. Stdio is
// left for the caller to attach.
func EditCmd(path string) (*exec.Cmd, error) {
	argv, _, err := Editor()
	if err != nil {
		return nil, err
	}
	return exec.Command(argv[0], append(argv[1:], path)...), nil
}
