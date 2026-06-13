package cli

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/template"
)

// Row is one renderable record: an ordered set of columns and the raw value
// behind them (used by json/jsonl output).
type Row struct {
	Cols  []string
	Vals  []string
	Value any
	URL   string
}

// Format is an output encoding.
type Format string

const (
	FormatAuto  Format = "auto"
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatCSV   Format = "csv"
	FormatTSV   Format = "tsv"
	FormatURL   Format = "url"
	FormatRaw   Format = "raw"
)

// Output renders rows in the chosen format, streaming where possible.
type Output struct {
	w        *bufio.Writer
	format   Format
	fields   []string
	noHeader bool
	tmpl     *template.Template

	wroteHead bool
	csvw      *csv.Writer
	jsonFirst bool
	table     [][]string
	tableCols []string
	count     int
}

// NewOutput builds an Output for the resolved format.
func NewOutput(w io.Writer, format Format, isTTY bool, fields []string, noHeader bool, tmpl string) (*Output, error) {
	if format == FormatAuto {
		if isTTY {
			format = FormatTable
		} else {
			format = FormatJSONL
		}
	}
	o := &Output{
		w:         bufio.NewWriter(w),
		format:    format,
		fields:    fields,
		noHeader:  noHeader,
		jsonFirst: true,
	}
	if tmpl != "" {
		t, err := template.New("row").Parse(tmpl)
		if err != nil {
			return nil, fmt.Errorf("bad --template: %w", err)
		}
		o.tmpl = t
		o.format = FormatTable // template overrides; we print its expansion verbatim
	}
	if format == FormatCSV {
		o.csvw = csv.NewWriter(o.w)
	}
	if format == FormatTSV {
		o.csvw = csv.NewWriter(o.w)
		o.csvw.Comma = '\t'
	}
	return o, nil
}

// project filters/reorders a row's columns per --fields.
func (o *Output) project(r Row) ([]string, []string) {
	if len(o.fields) == 0 {
		return r.Cols, r.Vals
	}
	index := make(map[string]string, len(r.Cols))
	for i, c := range r.Cols {
		if i < len(r.Vals) {
			index[c] = r.Vals[i]
		}
	}
	var raw map[string]any // lazily decoded for fields outside the column set
	cols := make([]string, 0, len(o.fields))
	vals := make([]string, 0, len(o.fields))
	for _, f := range o.fields {
		f = strings.TrimSpace(f)
		cols = append(cols, f)
		if v, ok := index[f]; ok {
			vals = append(vals, v)
			continue
		}
		// Fall back to the record's own JSON so any scraped field is reachable
		// by name, not just the handful promoted to table columns.
		if raw == nil && r.Value != nil {
			raw = map[string]any{}
			if b, err := json.Marshal(r.Value); err == nil {
				_ = json.Unmarshal(b, &raw)
			}
		}
		vals = append(vals, cellString(raw[f]))
	}
	return cols, vals
}

// cellString flattens a decoded JSON value into one table/CSV cell. Scalars
// render plainly, string lists join with ";", and anything structured (a list
// of ranks, a spec map) falls back to compact JSON.
func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, cellString(e))
		}
		return strings.Join(parts, ";")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

// Emit renders one row.
func (o *Output) Emit(r Row) error {
	o.count++
	if o.tmpl != nil {
		if err := o.tmpl.Execute(o.w, templateData(r)); err != nil {
			return err
		}
		_, err := o.w.WriteString("\n")
		return err
	}
	switch o.format {
	case FormatURL:
		_, err := o.w.WriteString(r.URL + "\n")
		return err
	case FormatRaw:
		b, _ := json.Marshal(r.Value)
		_, err := o.w.Write(append(b, '\n'))
		return err
	case FormatJSONL:
		b, err := json.Marshal(r.Value)
		if err != nil {
			return err
		}
		_, err = o.w.Write(append(b, '\n'))
		return err
	case FormatJSON:
		// bufio errors are sticky and surface at the final Write/Flush below.
		if o.jsonFirst {
			_, _ = o.w.WriteString("[\n")
			o.jsonFirst = false
		} else {
			_, _ = o.w.WriteString(",\n")
		}
		b, err := json.MarshalIndent(r.Value, "  ", "  ")
		if err != nil {
			return err
		}
		_, _ = o.w.WriteString("  ")
		_, err = o.w.Write(b)
		return err
	case FormatCSV, FormatTSV:
		cols, vals := o.project(r)
		if !o.wroteHead && !o.noHeader {
			if err := o.csvw.Write(cols); err != nil {
				return err
			}
			o.wroteHead = true
		}
		return o.csvw.Write(vals)
	default: // table — buffer to align columns at Close
		cols, vals := o.project(r)
		if o.tableCols == nil {
			o.tableCols = cols
		}
		o.table = append(o.table, vals)
		return nil
	}
}

// templateData builds the map a --template renders against. Keys are the
// record's JSON field names (so {{.asin}} works, matching --fields), decoded
// from the record itself; the column/value pairs fill any gaps.
func templateData(r Row) map[string]any {
	data := map[string]any{}
	if r.Value != nil {
		if b, err := json.Marshal(r.Value); err == nil {
			_ = json.Unmarshal(b, &data)
		}
	}
	for i, c := range r.Cols {
		if _, ok := data[c]; !ok && i < len(r.Vals) {
			data[c] = r.Vals[i]
		}
	}
	return data
}

// Count returns how many rows were emitted.
func (o *Output) Count() int { return o.count }

// Close flushes any buffered output (table alignment, JSON array close).
func (o *Output) Close() error {
	switch o.format {
	case FormatJSON:
		if o.jsonFirst {
			_, _ = o.w.WriteString("[]\n")
		} else {
			_, _ = o.w.WriteString("\n]\n")
		}
	case FormatCSV, FormatTSV:
		o.csvw.Flush()
	case FormatTable:
		if o.tmpl == nil {
			o.renderTable()
		}
	}
	return o.w.Flush()
}

func (o *Output) renderTable() {
	if len(o.table) == 0 {
		return
	}
	widths := make([]int, len(o.tableCols))
	for i, c := range o.tableCols {
		widths[i] = len(c)
	}
	for _, row := range o.table {
		for i, v := range row {
			if i < len(widths) && len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
	}
	if !o.noHeader {
		o.writeTableRow(upperAll(o.tableCols), widths)
	}
	for _, row := range o.table {
		o.writeTableRow(row, widths)
	}
}

func (o *Output) writeTableRow(cells []string, widths []int) {
	var b strings.Builder
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		b.WriteString(cell)
		if i < len(widths)-1 {
			for p := len(cell); p < w+2; p++ {
				b.WriteByte(' ')
			}
		}
	}
	_, _ = o.w.WriteString(strings.TrimRight(b.String(), " ") + "\n")
}

func upperAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToUpper(s)
	}
	return out
}
