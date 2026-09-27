package cli

import "strings"

// optionalValue is a string flag that may also be given bare.
//
// `--log-source` alone reads as "yes, all of it"; `--log-source=warn` names a
// level. Go's flag package has no optional-argument string, but it does honour
// IsBoolFlag, which is exactly the hook: when the flag appears bare the parser
// stops looking for a value instead of swallowing the next argument.
type optionalValue struct {
	set     bool
	value   string
	implied string
}

func newOptional(implied string) *optionalValue {
	return &optionalValue{implied: implied}
}

func (o *optionalValue) String() string {
	if o == nil {
		return ""
	}
	return o.value
}

func (o *optionalValue) Set(v string) error {
	o.set = true
	// A bare flag arrives as "true" from the boolean path.
	if strings.EqualFold(v, "true") {
		o.value = o.implied
		return nil
	}
	o.value = v
	return nil
}

// IsBoolFlag lets the flag stand alone.
func (o *optionalValue) IsBoolFlag() bool { return true }

// Value returns what was given, or "" when the flag was absent.
func (o *optionalValue) Value() string {
	if !o.set {
		return ""
	}
	return o.value
}

// repeatable collects a flag given several times.
//
// A bare "-" stands for whatever the setting inherited from configuration, so
// `--prefix - --prefix 'import x'` means "keep the configured lines, then add
// mine". Without a marker there is no way to say that: a repeated flag would
// either always replace or always append, and both are wanted.
type repeatable struct {
	values []any
}

func newRepeatable() *repeatable { return &repeatable{} }

func (r *repeatable) String() string { return "" }

func (r *repeatable) Set(v string) error {
	if v == "-" {
		r.values = append(r.values, nil)
		return nil
	}
	r.values = append(r.values, v)
	return nil
}

// Values returns what was given, nil when the flag was absent.
func (r *repeatable) Values() []any {
	if r == nil {
		return nil
	}
	return r.values
}
