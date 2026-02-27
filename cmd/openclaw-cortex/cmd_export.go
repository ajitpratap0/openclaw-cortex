package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func exportCmd() *cobra.Command {
	var (
		format string
		output string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export all memories to JSON or CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("export: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			// Paginate through all memories.
			var all []map[string]any
			cursor := ""
			for {
				memories, next, listErr := st.List(ctx, nil, 500, cursor)
				if listErr != nil {
					return fmt.Errorf("export: listing memories: %w", listErr)
				}
				for i := range memories {
					m := &memories[i]
					all = append(all, map[string]any{
						"id":           m.ID,
						"type":         string(m.Type),
						"scope":        string(m.Scope),
						"visibility":   string(m.Visibility),
						"content":      m.Content,
						"confidence":   m.Confidence,
						"source":       m.Source,
						"project":      m.Project,
						"tags":         m.Tags,
						"access_count": m.AccessCount,
						"created_at":   m.CreatedAt.Format("2006-01-02T15:04:05Z"),
						"updated_at":   m.UpdatedAt.Format("2006-01-02T15:04:05Z"),
					})
				}
				if next == "" {
					break
				}
				cursor = next
			}

			var w *os.File
			if output == "" || output == "-" {
				w = os.Stdout
			} else {
				w, err = os.Create(output)
				if err != nil {
					return fmt.Errorf("export: creating output file: %w", err)
				}
				defer func() { _ = w.Close() }()
			}

			switch format {
			case "json":
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(all); encErr != nil {
					return fmt.Errorf("export: encoding JSON: %w", encErr)
				}
			case "csv":
				cw := csv.NewWriter(w)
				headers := []string{"id", "type", "scope", "visibility", "content", "confidence", "source", "project", "access_count", "created_at"}
				if writeErr := cw.Write(headers); writeErr != nil {
					return fmt.Errorf("export: writing CSV header: %w", writeErr)
				}
				for _, m := range all {
					row := []string{
						fmt.Sprint(m["id"]),
						fmt.Sprint(m["type"]),
						fmt.Sprint(m["scope"]),
						fmt.Sprint(m["visibility"]),
						fmt.Sprint(m["content"]),
						fmt.Sprintf("%.4f", m["confidence"]),
						fmt.Sprint(m["source"]),
						fmt.Sprint(m["project"]),
						fmt.Sprint(m["access_count"]),
						fmt.Sprint(m["created_at"]),
					}
					if writeErr := cw.Write(row); writeErr != nil {
						return fmt.Errorf("export: writing CSV row: %w", writeErr)
					}
				}
				cw.Flush()
				if flushErr := cw.Error(); flushErr != nil {
					return fmt.Errorf("export: flushing CSV: %w", flushErr)
				}
			default:
				return fmt.Errorf("export: unsupported format %q (use json or csv)", format)
			}

			if output != "" && output != "-" {
				fmt.Fprintf(os.Stderr, "Exported %d memories to %s\n", len(all), output)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json or csv")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "output file path (- for stdout)")
	return cmd
}
