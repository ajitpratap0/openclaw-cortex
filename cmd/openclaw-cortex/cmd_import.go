package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// newJSONLScanner returns a Scanner pre-configured with a 10 MB buffer
// to handle large memory records that exceed the default 64KB limit.
func newJSONLScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1<<20), 10<<20) // initial 1MB, max 10MB
	return s
}

func importCmd() *cobra.Command {
	var (
		filePath string
		format   string
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import memories from a JSON or JSONL file",
		Long: `Import memories from a JSON array file or JSONL (JSON Lines) file.

The JSON format is a JSON array of memory objects matching the models.Memory struct.
The JSONL format is one memory object per line.

Use - as the file path to read from stdin.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			// Open input source.
			var r io.Reader
			if filePath == "" || filePath == "-" {
				r = os.Stdin
			} else {
				f, openErr := os.Open(filePath)
				if openErr != nil {
					return cmdErr("import: opening file", openErr)
				}
				defer func() { _ = f.Close() }()
				r = f
			}

			// Parse memories from the chosen format.
			var memories []models.Memory
			switch strings.ToLower(format) {
			case "json":
				dec := json.NewDecoder(r)
				if _, tokErr := dec.Token(); tokErr != nil {
					return cmdErr("import: parsing JSON array start", tokErr)
				}
				for dec.More() {
					var m models.Memory
					if decErr := dec.Decode(&m); decErr != nil {
						return cmdErr("import: reading JSON record", decErr)
					}
					memories = append(memories, m)
				}
			case "jsonl":
				scanner := newJSONLScanner(r)
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line == "" {
						continue
					}
					var m models.Memory
					if unmarshalErr := json.Unmarshal([]byte(line), &m); unmarshalErr != nil {
						return cmdErr("import: decoding JSONL line", unmarshalErr)
					}
					memories = append(memories, m)
				}
				if scanErr := scanner.Err(); scanErr != nil {
					return cmdErr("import: reading JSONL", scanErr)
				}
			default:
				return fmt.Errorf("import: unsupported format %q (use json or jsonl)", format)
			}

			// Connect to services.
			emb := newEmbedder(logger)
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("import: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			if err = st.EnsureCollection(ctx); err != nil {
				return cmdErr("import: ensuring collection", err)
			}

			// Upsert each memory.
			var imported, skipped int
			now := time.Now().UTC()
			for i := range memories {
				m := &memories[i]

				if strings.TrimSpace(m.Content) == "" {
					skipped++
					continue
				}

				// Back-fill timestamps if zero.
				if m.CreatedAt.IsZero() {
					m.CreatedAt = now
				}
				if m.UpdatedAt.IsZero() {
					m.UpdatedAt = now
				}
				if m.LastAccessed.IsZero() {
					m.LastAccessed = now
				}

				vec, embedErr := emb.Embed(ctx, m.Content)
				if embedErr != nil {
					fmt.Printf("Import stopped after %d memories (%d skipped): %v\n", imported, skipped, embedErr)
					return cmdErr("import: embedding memory", embedErr)
				}

				if upsertErr := st.Upsert(ctx, *m, vec); upsertErr != nil {
					fmt.Printf("Import stopped after %d memories (%d skipped): %v\n", imported, skipped, upsertErr)
					return cmdErr("import: upserting memory", upsertErr)
				}

				imported++
			}

			fmt.Printf("Imported %d memories (%d skipped)\n", imported, skipped)
			return nil
		},
	}

	cmd.Flags().StringVarP(&filePath, "file", "f", "-", "path to input file (- for stdin)")
	cmd.Flags().StringVar(&format, "format", "json", "input format: json or jsonl")
	return cmd
}
