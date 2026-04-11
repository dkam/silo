package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/dkam/silo/client"
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
			size = formatSize(e.Size)
		}
		mtime := ""
		if e.Mtime > 0 {
			mtime = time.Unix(e.Mtime, 0).Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%s  %10s  %-16s  %s\n", kind, size, mtime, e.Name)
	}
}

func formatSize(size int64) string {
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(size)/float64(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
