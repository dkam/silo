package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/dkam/silo/client"
	"github.com/dkam/silo/internal/format"
)

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printReposText(w io.Writer, repos []client.Repo) {
	for _, r := range repos {
		name := r.Name
		if name == "" {
			name = "(unnamed)"
		}
		updated := ""
		if r.UpdateTime > 0 {
			updated = time.Unix(r.UpdateTime, 0).Format("2006-01-02 15:04")
		}
		enc := ""
		if r.Encrypted {
			enc = " [encrypted]"
		}
		fmt.Fprintf(w, "%s  %-16s  %s%s\n", r.ID, updated, name, enc)
	}
}

func printDirText(w io.Writer, entries []client.DirEntry) {
	for _, e := range entries {
		kind := "f"
		if e.Type == "dir" {
			kind = "d"
		}
		size := "-"
		if e.Type != "dir" {
			size = format.Bytes(e.Size)
		}
		mtime := ""
		if e.Mtime > 0 {
			mtime = time.Unix(e.Mtime, 0).Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%s  %10s  %-16s  %s\n", kind, size, mtime, e.Name)
	}
}
