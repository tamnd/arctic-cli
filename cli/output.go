package cli

import (
	"io"

	"github.com/tamnd/arctic-cli/pkg/render"
)

// The output renderer lives in pkg/render so it stays reusable on its own. The
// cli refers to it through these aliases so command code reads the same as it
// did when the renderer was a cli-local type.
type Format = render.Format

const (
	FormatTable = render.FormatTable
	FormatJSON  = render.FormatJSON
	FormatJSONL = render.FormatJSONL
	FormatCSV   = render.FormatCSV
	FormatTSV   = render.FormatTSV
	FormatURL   = render.FormatURL
	FormatRaw   = render.FormatRaw
)

// NewRenderer builds a renderer writing to w.
func NewRenderer(w io.Writer, format Format, fields []string, noHeader bool, tmpl string) *render.Renderer {
	return render.New(w, format, fields, noHeader, tmpl)
}
