// Package humanize renders byte counts for UIs and logs. v1.1.4:
// consolidated — three identical private implementations lived in
// attachments, chunking and termshell, plus a variant in the tools
// archive package.
package humanize

import "fmt"

// Bytes renders a human size: "4.4 GB", "12 MB", "3.5 KB", "780 B".
func Bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
