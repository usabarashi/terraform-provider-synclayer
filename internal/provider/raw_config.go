package provider

import "github.com/hashicorp/go-cty/cty"

// configuredAttr returns the raw configured value of a resource attribute and
// whether it was explicitly set (to a non-empty value) in the configuration.
// known is false when the value is not yet known (e.g. it references an
// attribute of a resource that has not been created yet).
//
// It is used by CustomizeDiff functions, which run against the raw config
// before any provider defaulting or state carry-forward.
func configuredAttr(raw cty.Value, name string) (value string, configured, known bool) {
	if raw.IsNull() {
		return "", false, true
	}
	if !raw.IsKnown() || !raw.Type().IsObjectType() {
		return "", false, false
	}
	attr := raw.GetAttr(name)
	if !attr.IsKnown() {
		return "", false, false
	}
	if attr.IsNull() {
		return "", false, true
	}
	v := attr.AsString()
	return v, v != "", true
}

// attrConfigured reports whether an attribute is explicitly set (non-null and
// known) in the raw configuration. Unlike configuredAttr it makes no assumption
// about the attribute's type, so it can be used for bool/int attributes.
//
// known is false when the value is not yet known; callers treat that as "not
// configured" and leave the device value alone.
func attrConfigured(raw cty.Value, name string) (configured, known bool) {
	if raw.IsNull() {
		return false, true
	}
	if !raw.IsKnown() || !raw.Type().IsObjectType() {
		return false, false
	}
	attr := raw.GetAttr(name)
	if !attr.IsKnown() {
		return false, false
	}
	if attr.IsNull() {
		return false, true
	}
	return true, true
}
