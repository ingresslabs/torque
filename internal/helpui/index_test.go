package helpui

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildIndex_IncludesCommandsFlagsAndEnv(t *testing.T) {
	root := &cobra.Command{Use: "torque"}
	root.PersistentFlags().String("log-level", "info", "Log level")
	apply := &cobra.Command{Use: "apply", Short: "Apply chart", Example: "torque apply --chart ./chart --release foo"}
	apply.Flags().StringP("namespace", "n", "", "Namespace to deploy into")
	root.AddCommand(apply)

	index := BuildIndex(root, false)
	if len(index.Entries) == 0 {
		t.Fatalf("expected entries, got none")
	}
	assertHas := func(kind string, contains string) {
		t.Helper()
		for _, e := range index.Entries {
			if e.Kind != kind {
				continue
			}
			if e.Title == contains {
				return
			}
		}
		t.Fatalf("expected %s entry with title %q", kind, contains)
	}
	assertHas("command", "torque")
	assertHas("command", "torque apply")
	assertHas("env", "TORQUE_CONFIG")

	foundFlag := false
	for _, e := range index.Entries {
		if e.Kind == "flag" && e.Title == "-n, --namespace" {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Fatalf("expected flag entry for -n/--namespace")
	}
}

func TestBuildIndex_DeduplicatesGlobalFlags(t *testing.T) {
	root := &cobra.Command{Use: "torque"}
	root.PersistentFlags().String("mirror-bus", "", "Publish mirror payloads to a shared gRPC bus")
	a := &cobra.Command{Use: "a"}
	a.Flags().String("foo", "", "Foo")
	b := &cobra.Command{Use: "b"}
	b.Flags().String("bar", "", "Bar")
	root.AddCommand(a, b)

	index := BuildIndex(root, false)
	count := 0
	for _, e := range index.Entries {
		if e.Kind == "flag" && e.Title == "--mirror-bus" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 --mirror-bus entry, got %d", count)
	}
}

func TestBuildIndex_HasUniqueEntryIDs(t *testing.T) {
	root := &cobra.Command{Use: "torque"}
	root.AddCommand(&cobra.Command{Use: "apply", Short: "Apply chart"})

	index := BuildIndex(root, false)
	seen := make(map[string]struct{}, len(index.Entries))
	for _, entry := range index.Entries {
		if entry.ID == "" {
			t.Fatalf("entry with empty ID: %+v", entry)
		}
		if _, ok := seen[entry.ID]; ok {
			t.Fatalf("duplicate index entry ID %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
	}
}

func TestBuildIndex_IncludesDemosDoc(t *testing.T) {
	root := &cobra.Command{Use: "torque"}

	index := BuildIndex(root, false)
	for _, entry := range index.Entries {
		if entry.ID != "doc:demos" {
			continue
		}
		if entry.Title != "Demos" {
			t.Fatalf("unexpected demos title %q", entry.Title)
		}
		if !strings.Contains(entry.Content, "torque compared with split tooling") {
			t.Fatalf("expected demos content to include comparison demo")
		}
		return
	}
	t.Fatalf("expected demos doc in help index")
}
