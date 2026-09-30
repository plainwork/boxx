package envfile

import (
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
