package repo

import (
	"path/filepath"
	"testing"

	"go-backend/internal/store/model"
)

func TestTemplateBuilderBackupRoundTrip(t *testing.T) {
	source, err := Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	want := model.SubscriptionTemplate{Name: "visual", Kind: "clash", Content: "native profile", BuilderConfig: `{"version":1,"preset":"custom","groups":[],"rules":[],"final":"DIRECT"}`, CreatedTime: 1, UpdatedTime: 2}
	if err := source.SaveSubscriptionTemplate(&want); err != nil {
		t.Fatal(err)
	}
	backup, err := source.ExportPartial([]string{"inbounds"})
	if err != nil {
		t.Fatal(err)
	}
	dest, err := Open(filepath.Join(t.TempDir(), "dest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	for i := 0; i < 2; i++ {
		if _, err := dest.Import(backup, []string{"inbounds"}); err != nil {
			t.Fatal(err)
		}
		got, err := dest.GetSubscriptionTemplate(want.ID)
		if err != nil || got == nil || got.BuilderConfig != want.BuilderConfig || got.Content != want.Content || got.Name != want.Name {
			t.Fatalf("restored template: %+v %v", got, err)
		}
	}
}
