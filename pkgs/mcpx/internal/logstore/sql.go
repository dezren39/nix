package logstore

import (
	"fmt"
	"strings"
)

// readOnlyVerbs are the statements `mcpx log sql` will run.
//
// This is a guard rail, not a sandbox. The index can be deleted and rebuilt
// from the JSONL at any time, so nothing here is protecting data; it is
// protecting someone who typed DELETE meaning SELECT from a confusing
// afternoon. PRAGMA is allowed because that is how you ask SQLite about
// itself, and the mutating pragmas need a value that this refuses anyway.
var readOnlyVerbs = map[string]bool{
	"select": true, "explain": true, "pragma": true, "with": true, "values": true,
}

// CheckReadOnly rejects a statement that would change anything.
func CheckReadOnly(query string) error {
	stripped := stripSQLComments(query)
	trimmed := strings.TrimSpace(stripped)
	if trimmed == "" {
		return fmt.Errorf("empty query")
	}
	if strings.Contains(strings.TrimSuffix(strings.TrimSpace(trimmed), ";"), ";") {
		return fmt.Errorf("one statement at a time")
	}
	verb := strings.ToLower(strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ';'
	})[0])
	if !readOnlyVerbs[verb] {
		return fmt.Errorf("%q is not a read-only statement; use SELECT, WITH, VALUES, EXPLAIN or PRAGMA", verb)
	}
	// A CTE can hide a writer: WITH x AS (...) DELETE FROM records is valid
	// SQLite, and checking only the first word would wave it through.
	if verb == "with" {
		lower := strings.ToLower(trimmed)
		for _, bad := range []string{"delete", "insert", "update", "drop", "alter", "create", "replace"} {
			if containsWord(lower, bad) {
				return fmt.Errorf("%q appears in the query; only reads are allowed here", bad)
			}
		}
	}
	return nil
}

func containsWord(s, word string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], word)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !isWordByte(s[j-1])
		after := j+len(word) >= len(s) || !isWordByte(s[j+len(word)])
		if before && after {
			return true
		}
		i = j + len(word)
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// stripSQLComments removes -- and /* */ comments so a verb cannot be hidden
// behind one.
func stripSQLComments(q string) string {
	var b strings.Builder
	for i := 0; i < len(q); {
		switch {
		case strings.HasPrefix(q[i:], "--"):
			n := strings.IndexByte(q[i:], '\n')
			if n < 0 {
				return b.String()
			}
			i += n
		case strings.HasPrefix(q[i:], "/*"):
			n := strings.Index(q[i+2:], "*/")
			if n < 0 {
				return b.String()
			}
			i += n + 4
		default:
			b.WriteByte(q[i])
			i++
		}
	}
	return b.String()
}

// Table is the result of a raw query, already stringified for printing.
type Table struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// RunSQL executes a checked read-only statement.
func (s *Store) RunSQL(query string) (*Table, error) {
	if err := CheckReadOnly(query); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &Table{Columns: cols}
	for rows.Next() {
		cells := make([]any, len(cols))
		for i := range cells {
			cells[i] = new(any)
		}
		if err := rows.Scan(cells...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, c := range cells {
			row[i] = cellString(*(c.(*any)))
		}
		out.Rows = append(out.Rows, row)
	}
	return out, rows.Err()
}

func cellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	}
	return fmt.Sprint(v)
}
