package settings

import "fmt"

// Clamp is what bounding a requested value by its ceiling produced.
type Clamp struct {
	Value     string // what will be used
	Requested string // what was asked for
	By        *Value // the ceiling and its origin, when it lowered the request
}

// Lowered reports whether the ceiling changed the request.
func (c Clamp) Lowered() bool { return c.By != nil }

// ClampedBy renders the ceiling for a response: "autonomy.max (env:...)".
func (c Clamp) ClampedBy() string {
	if c.By == nil {
		return ""
	}
	return c.By.Path + " (" + c.By.Origin.String() + ")"
}

// Clamp bounds requested by the ClampedBy ceiling of path, as resolved in s.
// An empty requested means this set's own value for path. A handler that
// takes the dial from a request body calls this; the call-settings header
// gets the same through Value.
func (s *Set) Clamp(path, requested string) (Clamp, error) {
	decl, ok := s.schema.Lookup(path)
	if !ok {
		return Clamp{}, fmt.Errorf("unknown setting %q", path)
	}
	if requested == "" {
		v, _ := s.unclamped(path)
		requested = v.Raw
	}
	out := Clamp{Value: requested, Requested: requested}
	if decl.ClampedBy == "" {
		return out, nil
	}
	ceil, _ := s.Value(decl.ClampedBy)
	cdecl, _ := s.schema.Lookup(decl.ClampedBy)
	want, ok := cdecl.Rank(requested)
	if !ok {
		return Clamp{}, fmt.Errorf("%s: %q is not one of %v", path, requested, cdecl.Enum)
	}
	max, _ := cdecl.Rank(ceil.Raw)
	if want > max {
		out.Value = ceil.Raw
		out.By = ceil
	}
	return out, nil
}

// clampValue lowers v to its ceiling, recording what was asked for.
func (s *Set) clampValue(v *Value) *Value {
	decl, ok := s.schema.Lookup(v.Path)
	if !ok || decl.ClampedBy == "" {
		return v
	}
	c, err := s.Clamp(v.Path, v.Raw)
	if err != nil || !c.Lowered() {
		return v
	}
	out := *v
	out.Raw = c.Value
	out.Requested = c.Requested
	out.ClampedBy = c.ClampedBy()
	return &out
}

// Rank is the position of raw in an ordered setting.
func (d Setting) Rank(raw string) (int, bool) {
	for i, e := range d.Enum {
		if e == raw {
			return i, true
		}
	}
	return -1, false
}

// checkClamp refuses a ClampedBy that could not mean what it says.
func (s *Schema) checkClamp(d *Setting) error {
	if d.ClampedBy == "" {
		return nil
	}
	c, ok := s.byPath[d.ClampedBy]
	switch {
	case !ok:
		return fmt.Errorf("settings: %s is clamped by %q, which is not a setting", d.Path, d.ClampedBy)
	case c.ClampedBy != "":
		return fmt.Errorf("settings: %s's ceiling %s has a ceiling of its own", d.Path, c.Path)
	case c.Scope != ScopeDaemon || c.Hot:
		return fmt.Errorf("settings: %s's ceiling %s must be daemon-scoped and not hot, "+
			"or a caller could raise it", d.Path, c.Path)
	case d.Scope != ScopeCall:
		return fmt.Errorf("settings: %s is clamped, so it must be call-scoped", d.Path)
	case d.Kind != KindEnum || c.Kind != KindEnum:
		return fmt.Errorf("settings: %s and its ceiling %s must both be enums", d.Path, c.Path)
	}
	last := -1
	for _, e := range d.Enum {
		r, ok := c.Rank(e)
		if !ok || r <= last {
			return fmt.Errorf("settings: %s's values %v must be an ordered subset of %s's %v",
				d.Path, d.Enum, c.Path, c.Enum)
		}
		last = r
	}
	dr, _ := c.Rank(d.Default)
	cr, _ := c.Rank(c.Default)
	if dr > cr {
		return fmt.Errorf("settings: %s defaults to %q, above its ceiling %s's default %q",
			d.Path, d.Default, c.Path, c.Default)
	}
	return nil
}
