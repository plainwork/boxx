package envfile

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestEditor(t *testing.T) {
	t.Run("EDITOR with arguments", func(t *testing.T) {
		t.Setenv("EDITOR", "sh -c true")
		t.Setenv("VISUAL", "")
		argv, source, err := Editor()
		if err != nil || source != "$EDITOR" || !slices.Equal(argv, []string{"sh", "-c", "true"}) {
			t.Fatalf("got %v %q %v", argv, source, err)
		}
	})

	t.Run("VISUAL when EDITOR is unset", func(t *testing.T) {
		t.Setenv("EDITOR", "")
		t.Setenv("VISUAL", "sh")
		if _, source, err := Editor(); err != nil || source != "$VISUAL" {
			t.Fatalf("got %q %v", source, err)
		}
	})

	t.Run("EDITOR not installed", func(t *testing.T) {
		t.Setenv("EDITOR", "no-such-editor-boxx")
		if _, source, err := Editor(); err == nil || source != "$EDITOR" {
			t.Fatalf("got %q %v, want an error naming $EDITOR", source, err)
		}
	})

	t.Run("fallback", func(t *testing.T) {
		t.Setenv("EDITOR", "")
		t.Setenv("VISUAL", "")
		t.Setenv("PATH", t.TempDir())
		if _, _, err := Editor(); err == nil {
			t.Fatal("want an error when no editor is installed")
		}
	})
}

func TestKnownTerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERMINFO", dir)
	t.Setenv("TERMINFO_DIRS", "")
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x", "xterm-test"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for term, want := range map[string]bool{
		"xterm-test":          true,
		"xterm-no-such-thing": false,
		"":                    false,
		"../x/xterm-test":     false,
	} {
		if got := KnownTerminal(term); got != want {
			t.Errorf("KnownTerminal(%q) = %v, want %v", term, got, want)
		}
	}
}

func TestEditCmdTerm(t *testing.T) {
	t.Setenv("EDITOR", "sh")
	t.Setenv("TERMINFO", t.TempDir())
	t.Setenv("TERM", "xterm-no-such-thing")
	cmd, err := EditCmd("x.env")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cmd.Env, "TERM="+fallbackTerm) {
		t.Fatalf("want TERM=%s for an unknown terminal, env ends %v", fallbackTerm, cmd.Env[len(cmd.Env)-1:])
	}
}
