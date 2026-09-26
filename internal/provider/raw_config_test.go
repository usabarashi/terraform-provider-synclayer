package provider

import (
	"testing"

	"github.com/hashicorp/go-cty/cty"
)

func TestAttrConfigured(t *testing.T) {
	obj := cty.ObjectVal(map[string]cty.Value{
		"set":     cty.True,
		"false":   cty.False,
		"zero":    cty.NumberIntVal(0),
		"empty":   cty.StringVal(""),
		"unset":   cty.NullVal(cty.Bool),
		"unknown": cty.UnknownVal(cty.Bool),
	})

	cases := []struct {
		name       string
		raw        cty.Value
		attr       string
		configured bool
		known      bool
	}{
		{"null raw", cty.NullVal(cty.EmptyObject), "set", false, true},
		{"unknown raw", cty.UnknownVal(cty.EmptyObject), "set", false, false},
		{"non-object raw", cty.StringVal("x"), "set", false, false},
		{"explicit value", obj, "set", true, true},
		// An explicit false / 0 / "" is a configured value, unlike an absent one.
		{"explicit false", obj, "false", true, true},
		{"explicit zero", obj, "zero", true, true},
		{"explicit empty string", obj, "empty", true, true},
		{"null value", obj, "unset", false, true},
		{"unknown value", obj, "unknown", false, false},
	}

	for _, c := range cases {
		configured, known := attrConfigured(c.raw, c.attr)
		if configured != c.configured || known != c.known {
			t.Errorf("%s: got (configured=%v, known=%v), want (configured=%v, known=%v)",
				c.name, configured, known, c.configured, c.known)
		}
	}
}

func TestConfiguredAttr(t *testing.T) {
	obj := cty.ObjectVal(map[string]cty.Value{
		"set":     cty.StringVal("x"),
		"empty":   cty.StringVal(""),
		"blank":   cty.StringVal(" "),
		"unset":   cty.NullVal(cty.String),
		"unknown": cty.UnknownVal(cty.String),
	})

	cases := []struct {
		attr       string
		value      string
		configured bool
		known      bool
	}{
		{"set", "x", true, true},
		{"empty", "", false, true},
		{"blank", " ", true, true},
		{"unset", "", false, true},
		{"unknown", "", false, false},
	}

	for _, c := range cases {
		value, configured, known := configuredAttr(obj, c.attr)
		if value != c.value || configured != c.configured || known != c.known {
			t.Errorf("%s: got (%q, configured=%v, known=%v), want (%q, configured=%v, known=%v)",
				c.attr, value, configured, known, c.value, c.configured, c.known)
		}
	}

	if _, configured, known := configuredAttr(cty.NullVal(cty.EmptyObject), "set"); configured || !known {
		t.Errorf("null raw: got (configured=%v, known=%v), want (false, true)", configured, known)
	}
	if _, configured, known := configuredAttr(cty.UnknownVal(cty.EmptyObject), "set"); configured || known {
		t.Errorf("unknown raw: got (configured=%v, known=%v), want (false, false)", configured, known)
	}
}
