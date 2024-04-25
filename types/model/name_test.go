package model

import (
	"reflect"
	"strings"
	"testing"
)

const (
	part80  = "88888888888888888888888888888888888888888888888888888888888888888888888888888888"
	part350 = "33333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333333"
)

func TestParseNameParts(t *testing.T) {
	cases := []struct {
		in              string
		want            Name
		wantValidDigest bool
	}{
		{
			in: "host/namespace/model:tag",
			want: Name{
				Host:      "host",
				Namespace: "namespace",
				Model:     "model",
				Tag:       "tag",
			},
		},
		{
			in: "host/namespace/model",
			want: Name{
				Host:      "host",
				Namespace: "namespace",
				Model:     "model",
			},
		},
		{
			in: "namespace/model",
			want: Name{
				Namespace: "namespace",
				Model:     "model",
			},
		},
		{
			in: "model",
			want: Name{
				Model: "model",
			},
		},
		{
			in: "h/nn/mm:t",
			want: Name{
				Host:      "h",
				Namespace: "nn",
				Model:     "mm",
				Tag:       "t",
			},
		},
		{
			in: part80 + "/" + part80 + "/" + part80 + ":" + part80,
			want: Name{
				Host:      part80,
				Namespace: part80,
				Model:     part80,
				Tag:       part80,
			},
		},
		{
			in: part350 + "/" + part80 + "/" + part80 + ":" + part80,
			want: Name{
				Host:      part350,
				Namespace: part80,
				Model:     part80,
				Tag:       part80,
			},
		},
		{
			in: "@digest",
			want: Name{
				RawDigest: "digest",
			},
			wantValidDigest: false,
		},
		{
			in: "model@sha256:" + validSHA256Hex,
			want: Name{
				Model:     "model",
				RawDigest: "sha256:" + validSHA256Hex,
			},
			wantValidDigest: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			got := ParseNameNoDefaults(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseNameNoDefaults(%q) = %v; want %v", tt.in, got, tt.want)
			}
			if got.Digest().IsValid() != tt.wantValidDigest {
				t.Errorf("ParseNameNoDefaults(%q).Digest().IsValid() = %v; want %v", tt.in, got.Digest().IsValid(), tt.wantValidDigest)
			}
		})
	}
}

var testCases = map[string]bool{ // name -> valid
	"host/namespace/model:tag": true,
	"host/namespace/model":     true,
	"namespace/model":          true,
	"model":                    true,
	"@dd":                      true,
	"model@dd":                 true,

	// long (but valid)
	part80 + "/" + part80 + "/" + part80 + ":" + part80:  true,
	part350 + "/" + part80 + "/" + part80 + ":" + part80: true,

	"h/nn/mm:t@dd": true, // bare minimum part sizes

	"m":        false, // model too short
	"n/mm:":    false, // namespace too short
	"h/n/mm:t": false, // namespace too short
	"@t":       false, // digest too short
	"mm@d":     false, // digest too short

	// invalids
	"^":      false,
	"mm:":    false,
	"/nn/mm": false,
	"//":     false,
	"//mm":   false,
	"hh//":   false,
	"//mm:@": false,
	"00@":    false,
	"@":      false,

	// not starting with alphanum
	"-hh/nn/mm:tt@dd": false,
	"hh/-nn/mm:tt@dd": false,
	"hh/nn/-mm:tt@dd": false,
	"hh/nn/mm:-tt@dd": false,
	"hh/nn/mm:tt@-dd": false,

	"": false,

	// hosts
	"host:https/namespace/model:tag": true,

	// colon in non-host part before tag
	"host/name:space/model:tag": false,
}

func TestParseNameDefault(t *testing.T) {
	n := ParseName("x")
	t.Logf("ParseName(%q) = %#v", "x", n)
	got := n.String()
	want := "registry.ollama.ai/library/x:latest"
	if got != want {
		t.Errorf("ParseName(%q).String() = %q; want %q", "x", got, want)
	}
}

func TestIsValid(t *testing.T) {
	var numStringTests int
	for s, want := range testCases {
		n := ParseNameNoDefaults(s)
		t.Logf("ParseName(%q) = %+v", s, n)
		got := n.IsValid()
		if got != want {
			t.Errorf("ParseName(%q).IsValid() = %v; want %v", s, got, want)
		}

		// Test roundtrip with String
		if got {
			got := ParseNameNoDefaults(s).String()
			if got != s {
				t.Errorf("ParseName(%q).String() = %q; want %q", s, got, s)
			}
			numStringTests++
		}
	}

	if numStringTests == 0 {
		t.Errorf("no tests for Name.String")
	}
}

func TestIsValidPart(t *testing.T) {
	cases := []struct {
		kind partKind
		s    string
		want bool
	}{
		{kind: kindHost, s: "", want: false},
		{kind: kindHost, s: "a", want: true},
		{kind: kindHost, s: "a.", want: true},
		{kind: kindHost, s: "a.b", want: true},
		{kind: kindHost, s: "a:123", want: true},
		{kind: kindHost, s: "a:123/aa/bb", want: false},
		{kind: kindNamespace, s: "bb", want: true},
		{kind: kindNamespace, s: "a.", want: false},
		{kind: kindModel, s: "-h", want: false},
		{kind: kindDigest, s: "dd", want: true},
	}
	for _, tt := range cases {
		t.Run(tt.s, func(t *testing.T) {
			got := isValidPart(tt.kind, tt.s)
			if got != tt.want {
				t.Errorf("isValidPart(%s, %q) = %v; want %v", tt.kind, tt.s, got, tt.want)
			}
		})
	}

}

func TestIsValidShort(t *testing.T) {
	check := func(namespace, mode string) {
		t.Helper()
		got := IsValidShort(namespace, mode)
		want := Name{Namespace: namespace, Model: mode}.IsValid()
		if got != want {
			t.Errorf("IsValidShort(%q, %q) = %v; want %v", namespace, mode, got, want)
		}
	}
	check("n", "m")
	check("n", "mm")
	check("nn", "m")
}

func FuzzName(f *testing.F) {
	for s := range testCases {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n := ParseNameNoDefaults(s)
		if n.IsValid() {
			parts := [...]string{n.Host, n.Namespace, n.Model, n.Tag, n.RawDigest}
			for _, part := range parts {
				if part == ".." {
					t.Errorf("unexpected .. as valid part")
				}
				if len(part) > 350 {
					t.Errorf("part too long: %q", part)
				}
			}
			if n.String() != s {
				t.Errorf("String() = %q; want %q", n.String(), s)
			}
		}

	})
}

const validSHA256Hex = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

func TestParseDigest(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{in: "sha256:" + validSHA256Hex, want: true},
		{in: "sha256-" + validSHA256Hex, want: true},

		{in: "", want: false},
		{in: "sha134:" + validSHA256Hex, want: false},
		{in: "sha256:" + validSHA256Hex + "x", want: false},
		{in: "sha256:x" + validSHA256Hex, want: false},
		{in: "sha256-" + validSHA256Hex + "x", want: false},
		{in: "sha256-x", want: false},
	}

	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			d := ParseDigest(tt.in)
			if d.IsValid() != tt.want {
				t.Errorf("ParseDigest(%q).IsValid() = %v; want %v", tt.in, d.IsValid(), tt.want)
			}
			norm := strings.ReplaceAll(tt.in, ":", "-")
			if d.IsValid() && d.String() != norm {
				t.Errorf("ParseDigest(%q).String() = %q; want %q", tt.in, d.String(), norm)
			}
		})
	}
}

func TestDigestString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "sha256:" + validSHA256Hex, want: "sha256-" + validSHA256Hex},
		{in: "sha256-" + validSHA256Hex, want: "sha256-" + validSHA256Hex},
		{in: "", want: "unknown-0000000000000000000000000000000000000000000000000000000000000000"},
		{in: "blah-100000000000000000000000000000000000000000000000000000000000000", want: "unknown-0000000000000000000000000000000000000000000000000000000000000000"},
	}

	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			d := ParseDigest(tt.in)
			if d.String() != tt.want {
				t.Errorf("ParseDigest(%q).String() = %q; want %q", tt.in, d.String(), tt.want)
			}
		})
	}
}
