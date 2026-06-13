// Package render turns slices of record structs into one of the output formats
// arctic supports: table, json, jsonl, csv, tsv, url, and raw. It works off
// struct reflection and json tags, so any record type renders without
// per-type code. It carries no Reddit knowledge and is reusable on its own.
package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"
)

// Format is an output rendering format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatCSV   Format = "csv"
	FormatTSV   Format = "tsv"
	FormatURL   Format = "url"
	FormatRaw   Format = "raw"
)

// Valid reports whether f is one of the supported formats.
func (f Format) Valid() bool {
	switch f {
	case FormatTable, FormatJSON, FormatJSONL, FormatCSV, FormatTSV, FormatURL, FormatRaw:
		return true
	}
	return false
}

// Renderer writes records in a chosen format. Fields, when set, picks and orders
// the columns by their json key; NoHeader drops the header row; Template runs a
// Go text/template per record instead of a fixed format.
type Renderer struct {
	Format   Format
	Fields   []string
	NoHeader bool
	Template string
	w        io.Writer
}

// New builds a Renderer writing to w.
func New(w io.Writer, format Format, fields []string, noHeader bool, tmpl string) *Renderer {
	return &Renderer{Format: format, Fields: fields, NoHeader: noHeader, Template: tmpl, w: w}
}

// Render writes records (a slice of structs, or a single struct) in the
// configured format.
func (r *Renderer) Render(records any) error {
	rv := reflect.ValueOf(records)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Slice {
		s := reflect.MakeSlice(reflect.SliceOf(rv.Type()), 1, 1)
		s.Index(0).Set(rv)
		rv = s
	}
	n := rv.Len()
	items := make([]any, n)
	for i := 0; i < n; i++ {
		items[i] = rv.Index(i).Interface()
	}

	if r.Template != "" {
		return r.renderTemplate(items)
	}
	switch r.Format {
	case FormatJSON:
		return r.renderJSON(items)
	case FormatJSONL:
		return r.renderJSONL(items)
	case FormatCSV:
		return r.renderDelimited(items, ',')
	case FormatTSV:
		return r.renderDelimited(items, '\t')
	case FormatURL:
		return r.renderURL(items)
	case FormatRaw:
		return r.renderRaw(items)
	default:
		return r.renderTable(items)
	}
}

func (r *Renderer) renderJSON(items []any) error {
	if len(r.Fields) == 0 {
		enc := json.NewEncoder(r.w)
		enc.SetIndent("", "  ")
		if len(items) == 1 {
			return enc.Encode(items[0])
		}
		return enc.Encode(items)
	}
	var compact bytes.Buffer
	if len(items) == 1 {
		b, err := r.orderedObject(items[0])
		if err != nil {
			return err
		}
		compact.Write(b)
	} else {
		compact.WriteByte('[')
		for i, it := range items {
			if i > 0 {
				compact.WriteByte(',')
			}
			b, err := r.orderedObject(it)
			if err != nil {
				return err
			}
			compact.Write(b)
		}
		compact.WriteByte(']')
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, compact.Bytes(), "", "  "); err != nil {
		return err
	}
	pretty.WriteByte('\n')
	_, err := r.w.Write(pretty.Bytes())
	return err
}

func (r *Renderer) renderJSONL(items []any) error {
	if len(r.Fields) == 0 {
		enc := json.NewEncoder(r.w)
		for _, it := range items {
			if err := enc.Encode(it); err != nil {
				return err
			}
		}
		return nil
	}
	for _, it := range items {
		b, err := r.orderedObject(it)
		if err != nil {
			return err
		}
		if _, err := r.w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// orderedObject marshals an item's --fields columns as a compact JSON object,
// in the order the columns were given, so json and jsonl project the same
// subset that table, csv, and tsv do. A column the record does not carry is
// skipped rather than emitted as null.
func (r *Renderer) orderedObject(item any) ([]byte, error) {
	full, ok := toAnyMap(item).(map[string]any)
	if !ok {
		return json.Marshal(item)
	}
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, c := range r.Fields {
		v, present := full[c]
		if !present {
			continue
		}
		key, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.Write(key)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (r *Renderer) renderTemplate(items []any) error {
	t, err := template.New("row").Funcs(template.FuncMap{
		"join": func(sep string, v any) string { return joinAny(sep, v) },
	}).Parse(r.Template)
	if err != nil {
		return fmt.Errorf("parse --template: %w", err)
	}
	for _, it := range items {
		// Execute against the json-keyed view so a template reads the same
		// lowercase keys (.title, .score) as --fields and the table header,
		// rather than the Go struct field names.
		if err := t.Execute(r.w, toAnyMap(it)); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(r.w)
	}
	return nil
}

func (r *Renderer) renderURL(items []any) error {
	for _, it := range items {
		m := toMap(it)
		if u := firstNonEmpty(m["url"], m["permalink"]); u != "" {
			_, _ = fmt.Fprintln(r.w, u)
		}
	}
	return nil
}

func (r *Renderer) renderRaw(items []any) error {
	cols := r.columns(items)
	for _, it := range items {
		m := toMap(it)
		vals := make([]string, 0, len(cols))
		for _, c := range cols {
			vals = append(vals, m[c])
		}
		_, _ = fmt.Fprintln(r.w, strings.Join(vals, " "))
	}
	return nil
}

func (r *Renderer) renderTable(items []any) error {
	if len(items) == 0 {
		return nil
	}
	cols := r.columns(items)
	tw := tabwriter.NewWriter(r.w, 0, 4, 2, ' ', 0)
	if !r.NoHeader {
		_, _ = fmt.Fprintln(tw, strings.Join(upperAll(cols), "\t"))
	}
	for _, it := range items {
		m := toMap(it)
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = truncate(m[c], 60)
		}
		_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

func (r *Renderer) renderDelimited(items []any, comma rune) error {
	if len(items) == 0 {
		return nil
	}
	cols := r.columns(items)
	cw := csv.NewWriter(r.w)
	cw.Comma = comma
	if !r.NoHeader {
		if err := cw.Write(cols); err != nil {
			return err
		}
	}
	for _, it := range items {
		m := toMap(it)
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = m[c]
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// columns returns the column order: explicit Fields when set, otherwise the
// first record's json field order.
func (r *Renderer) columns(items []any) []string {
	if len(r.Fields) > 0 {
		return r.Fields
	}
	if len(items) == 0 {
		return nil
	}
	return structJSONKeys(items[0])
}

// toAnyMap renders an item to its json-keyed view with values left in their
// natural JSON types (string, number, bool, slice, object), so a template can
// read .title as text and .score as a number and range over a slice field.
func toAnyMap(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		// Not a json object (e.g. a bare scalar); hand the value through as-is.
		return v
	}
	return m
}

// joinAny joins a slice value (as produced by toAnyMap, i.e. []any of strings)
// with sep, so a template can write {{join "," .tags}}.
func joinAny(sep string, v any) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case []string:
		return strings.Join(vv, sep)
	case []any:
		parts := make([]string, len(vv))
		for i, e := range vv {
			parts[i] = fmt.Sprintf("%v", e)
		}
		return strings.Join(parts, sep)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toMap(v any) map[string]string {
	out := map[string]string{}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return out
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		key := jsonKey(f)
		if key == "-" {
			continue
		}
		out[key] = formatValue(rv.Field(i))
	}
	return out
}

func structJSONKeys(v any) []string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	rt := rv.Type()
	var keys []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		key := jsonKey(f)
		if key == "-" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func jsonKey(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return f.Name
	}
	return name
}

func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts[i] = formatValue(v.Index(i))
		}
		return strings.Join(parts, ";")
	case reflect.Struct:
		if t, ok := v.Interface().(time.Time); ok {
			if t.IsZero() {
				return ""
			}
			return t.Format(time.RFC3339)
		}
	}
	return fmt.Sprintf("%v", v.Interface())
}

func upperAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToUpper(s)
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n-1]) + "…"
}
