package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ingresslabs/torque/internal/capture"
	"github.com/spf13/cobra"
)

func newExplainCommand() *cobra.Command {
	var format string
	var sessionID string
	var maxHints int

	cmd := &cobra.Command{
		Use:   "explain CAPTURE.sqlite",
		Short: "Explain a captured torque run",
		Long:  "Explain a captured torque deploy, build, log, or stack run from a portable SQLite evidence file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := capture.Summarize(cmd.Context(), args[0], capture.SummaryOptions{
				SessionID:       sessionID,
				MaxFailureHints: maxHints,
			})
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "text":
				return writeExplainText(cmd.OutOrStdout(), summary)
			case "md", "markdown":
				return writeExplainMarkdown(cmd.OutOrStdout(), summary)
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(summary)
			default:
				return fmt.Errorf("unsupported --format %q (expected text, markdown, or json)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text, markdown, or json")
	cmd.Flags().StringVar(&sessionID, "session", "", "Explain only this session_id or run_id")
	cmd.Flags().IntVar(&maxHints, "max-hints", 8, "Maximum failure hints to print per session")
	decorateCommandHelp(cmd, "Explain Flags")
	cmd.Example = `  # Explain a deploy capture
  torque explain ./apply.sqlite

  # Emit machine-readable evidence summary
  torque explain ./stack.sqlite --format json`
	return cmd
}

func writeExplainText(w io.Writer, summary *capture.CaptureSummary) error {
	if summary == nil {
		return nil
	}
	fmt.Fprintf(w, "Capture: %s\n", summary.Path)
	fmt.Fprintf(w, "Sessions: %d\n", len(summary.Sessions))
	for i, sess := range summary.Sessions {
		if i > 0 {
			fmt.Fprintln(w)
		}
		writeExplainSession(w, sess)
	}
	return nil
}

func writeExplainSession(w io.Writer, sess capture.SessionSummary) {
	fmt.Fprintf(w, "\nSession %s\n", sess.SessionID)
	if sess.RunID != "" {
		fmt.Fprintf(w, "  Run: %s\n", sess.RunID)
	}
	fmt.Fprintf(w, "  Command: %s\n", firstNonEmpty(strings.Join(sess.Args, " "), sess.Command))
	if !sess.StartedAt.IsZero() {
		fmt.Fprintf(w, "  Started: %s\n", sess.StartedAt.Format("2006-01-02 15:04:05 MST"))
	}
	if sess.EndedAt != nil {
		fmt.Fprintf(w, "  Ended: %s", sess.EndedAt.Format("2006-01-02 15:04:05 MST"))
		if sess.Duration != "" {
			fmt.Fprintf(w, " (%s)", sess.Duration)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "  Outcome: %s\n", sess.Outcome)
	if sess.PrimaryCause != nil {
		fmt.Fprintf(w, "  Primary cause: %s: %s\n", sess.PrimaryCause.Category, sess.PrimaryCause.Message)
		if sess.PrimaryCause.Resource != "" {
			fmt.Fprintf(w, "  Affected resource: %s\n", sess.PrimaryCause.Resource)
		}
	}
	if scope := explainScope(sess.Entities); scope != "" {
		fmt.Fprintf(w, "  Scope: %s\n", scope)
	}
	if sess.ChangeSummary.HasDiff || sess.ChangeSummary.ManifestResources > 0 || sess.ChangeSummary.ManifestBytes > 0 {
		fmt.Fprintln(w, "  Change summary:")
		if sess.ChangeSummary.HasDiff {
			fmt.Fprintf(w, "    Diff lines: %d\n", sess.ChangeSummary.DiffLines)
		}
		if sess.ChangeSummary.ManifestResources > 0 {
			fmt.Fprintf(w, "    Manifest resources: %d\n", sess.ChangeSummary.ManifestResources)
		}
		if sess.ChangeSummary.ManifestBytes > 0 {
			fmt.Fprintf(w, "    Manifest bytes: %d\n", sess.ChangeSummary.ManifestBytes)
		}
	}
	if sess.DroppedEvents > 0 {
		fmt.Fprintf(w, "  Dropped events: %d\n", sess.DroppedEvents)
	}
	if sess.BuildDigest != "" || len(sess.BuildTags) > 0 || sess.BuildPolicy != nil {
		fmt.Fprintln(w, "  Build evidence:")
		if sess.BuildDigest != "" {
			fmt.Fprintf(w, "    Digest: %s\n", sess.BuildDigest)
		}
		if len(sess.BuildTags) > 0 {
			fmt.Fprintf(w, "    Tags: %s\n", strings.Join(sess.BuildTags, ", "))
		}
		if sess.BuildPolicy != nil {
			fmt.Fprintf(w, "    Policy: passed=%t deny=%d warn=%d\n", sess.BuildPolicy.Passed, sess.BuildPolicy.DenyCount, sess.BuildPolicy.WarnCount)
		}
	}
	if sess.StackNodeCount > 0 {
		fmt.Fprintf(w, "  Stack plan: %d nodes\n", sess.StackNodeCount)
	}
	fmt.Fprintf(w, "  Evidence: %d events, %d artifacts\n", sess.EventCount, sess.ArtifactCount)
	if len(sess.EventKinds) > 0 {
		fmt.Fprintf(w, "  Event kinds: %s\n", formatCounts(sess.EventKinds))
	}
	if len(sess.FailureHints) > 0 {
		fmt.Fprintln(w, "  Failure hints:")
		for _, hint := range sess.FailureHints {
			fmt.Fprintf(w, "    - %s\n", formatFailureHint(hint))
		}
	} else {
		fmt.Fprintln(w, "  Failure hints: none found in captured events")
	}
	if len(sess.ResourceHints) > 0 {
		fmt.Fprintln(w, "  Likely resources:")
		for _, hint := range sess.ResourceHints {
			fmt.Fprintf(w, "    - %s (%d signals)\n", hint.Resource, hint.Count)
		}
	}
	if len(sess.Causes) > 0 {
		fmt.Fprintln(w, "  Cause details:")
		for _, cause := range sess.Causes {
			fmt.Fprintf(w, "    - %s [%s]", cause.Category, cause.Severity)
			if cause.Resource != "" {
				fmt.Fprintf(w, " %s", cause.Resource)
			}
			fmt.Fprintf(w, ": %s\n", cause.Message)
			for _, fix := range cause.Fixes {
				fmt.Fprintf(w, "      fix: %s\n", fix)
			}
		}
	}
	if len(sess.FixCommands) > 0 {
		fmt.Fprintln(w, "  Next commands:")
		for _, suggestion := range sess.FixCommands {
			fmt.Fprintf(w, "    - [%s] %s\n", suggestion.Purpose, suggestion.Command)
		}
	}
	if len(sess.ArtifactNames) > 0 {
		fmt.Fprintf(w, "  Artifacts: %s\n", strings.Join(sess.ArtifactNames, ", "))
	}
}

func writeExplainMarkdown(w io.Writer, summary *capture.CaptureSummary) error {
	if summary == nil {
		return nil
	}
	fmt.Fprintf(w, "# torque explain\n\n")
	fmt.Fprintf(w, "- Capture: `%s`\n", summary.Path)
	fmt.Fprintf(w, "- Sessions: `%d`\n\n", len(summary.Sessions))
	for _, sess := range summary.Sessions {
		fmt.Fprintf(w, "## Session `%s`\n\n", sess.SessionID)
		fmt.Fprintf(w, "| Field | Value |\n| --- | --- |\n")
		fmt.Fprintf(w, "| Outcome | `%s` |\n", sess.Outcome)
		fmt.Fprintf(w, "| Command | `%s` |\n", markdownEscape(firstNonEmpty(strings.Join(sess.Args, " "), sess.Command)))
		if sess.RunID != "" {
			fmt.Fprintf(w, "| Run | `%s` |\n", markdownEscape(sess.RunID))
		}
		if scope := explainScope(sess.Entities); scope != "" {
			fmt.Fprintf(w, "| Scope | `%s` |\n", markdownEscape(scope))
		}
		fmt.Fprintf(w, "| Evidence | `%d events, %d artifacts` |\n", sess.EventCount, sess.ArtifactCount)
		if sess.PrimaryCause != nil {
			fmt.Fprintf(w, "| Primary cause | `%s: %s` |\n", markdownEscape(sess.PrimaryCause.Category), markdownEscape(sess.PrimaryCause.Message))
		}
		fmt.Fprintln(w)
		if sess.ChangeSummary.HasDiff || sess.ChangeSummary.ManifestResources > 0 {
			fmt.Fprintln(w, "### Change Summary")
			fmt.Fprintln(w)
			fmt.Fprintf(w, "- Diff lines: `%d`\n", sess.ChangeSummary.DiffLines)
			fmt.Fprintf(w, "- Manifest resources: `%d`\n", sess.ChangeSummary.ManifestResources)
			fmt.Fprintf(w, "- Manifest bytes: `%d`\n\n", sess.ChangeSummary.ManifestBytes)
		}
		if len(sess.Causes) > 0 {
			fmt.Fprintln(w, "### Causes")
			fmt.Fprintln(w)
			for _, cause := range sess.Causes {
				fmt.Fprintf(w, "- `%s` `%s`", cause.Category, cause.Severity)
				if cause.Resource != "" {
					fmt.Fprintf(w, " `%s`", markdownEscape(cause.Resource))
				}
				fmt.Fprintf(w, ": %s\n", markdownEscape(cause.Message))
				for _, fix := range cause.Fixes {
					fmt.Fprintf(w, "  - Fix: %s\n", markdownEscape(fix))
				}
			}
			fmt.Fprintln(w)
		}
		if len(sess.FailureHints) > 0 {
			fmt.Fprintln(w, "### Failure Hints")
			fmt.Fprintln(w)
			for _, hint := range sess.FailureHints {
				fmt.Fprintf(w, "- %s\n", markdownEscape(formatFailureHint(hint)))
			}
			fmt.Fprintln(w)
		}
		if len(sess.FixCommands) > 0 {
			fmt.Fprintln(w, "### Next Commands")
			fmt.Fprintln(w)
			for _, suggestion := range sess.FixCommands {
				fmt.Fprintf(w, "- **%s**\n\n  ```bash\n  %s\n  ```\n", markdownEscape(suggestion.Purpose), suggestion.Command)
			}
		}
	}
	return nil
}

func explainScope(ent capture.Entities) string {
	var parts []string
	if ent.KubeContext != "" {
		parts = append(parts, "context="+ent.KubeContext)
	}
	if ent.Namespace != "" {
		parts = append(parts, "namespace="+ent.Namespace)
	}
	if ent.Release != "" {
		parts = append(parts, "release="+ent.Release)
	}
	if ent.Chart != "" {
		parts = append(parts, "chart="+ent.Chart)
	}
	if ent.ImageRef != "" {
		parts = append(parts, "image="+ent.ImageRef)
	}
	if ent.ImageDigest != "" {
		parts = append(parts, "digest="+ent.ImageDigest)
	}
	if ent.BuildContext != "" {
		parts = append(parts, "buildContext="+ent.BuildContext)
	}
	return strings.Join(parts, " ")
}

func formatCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func formatFailureHint(h capture.FailureHint) string {
	var prefix []string
	if h.Kind != "" {
		prefix = append(prefix, h.Kind)
	}
	if h.Source != "" {
		prefix = append(prefix, h.Source)
	}
	if h.Level != "" {
		prefix = append(prefix, h.Level)
	}
	if h.Namespace != "" {
		prefix = append(prefix, "ns/"+h.Namespace)
	}
	if h.Pod != "" {
		prefix = append(prefix, "pod/"+h.Pod)
	}
	if h.Container != "" {
		prefix = append(prefix, "container/"+h.Container)
	}
	msg := strings.TrimSpace(h.Message)
	if msg == "" {
		msg = "(no message)"
	}
	if len(prefix) == 0 {
		return msg
	}
	return strings.Join(prefix, " ") + ": " + msg
}

func markdownEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
