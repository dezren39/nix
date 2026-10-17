package cli

import (
	"reflect"
	"testing"
)

// These are the lines `systemctl --user show --property=ExecStart --value`
// prints, with the wrapper the mcpx unit runs ahead of the binary.
func TestUnitStartPrefixIsWhatComesBeforeTheBinary(t *testing.T) {
	cases := map[string]struct {
		show string
		want []string
	}{
		"a wrapper ahead of the binary": {
			show: "{ path=/nix/store/aaa-mcpx-start ; argv[]=/nix/store/aaa-mcpx-start /nix/store/bbb-mcpx/bin/mcpx daemon ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }",
			want: []string{"/nix/store/aaa-mcpx-start"},
		},
		"two wrappers": {
			show: "{ path=/nix/store/ccc-env ; argv[]=/nix/store/ccc-env /nix/store/aaa-mcpx-start /nix/store/bbb-mcpx/bin/mcpx daemon ; ignore_errors=no }",
			want: []string{"/nix/store/ccc-env", "/nix/store/aaa-mcpx-start"},
		},
		"the binary started directly": {
			show: "{ path=/nix/store/bbb-mcpx/bin/mcpx ; argv[]=/nix/store/bbb-mcpx/bin/mcpx daemon ; ignore_errors=no }",
			want: nil,
		},
		"another program": {
			show: "{ path=/bin/sh ; argv[]=/bin/sh -c true ; ignore_errors=no }",
			want: nil,
		},
		"no unit": {
			show: "",
			want: nil,
		},
		"a value with no argv": {
			show: "/nix/store/aaa-mcpx-start /nix/store/bbb-mcpx/bin/mcpx daemon",
			want: nil,
		},
	}
	for name, c := range cases {
		if got := startPrefix(c.show); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: startPrefix = %q, want %q", name, got, c.want)
		}
	}
}
