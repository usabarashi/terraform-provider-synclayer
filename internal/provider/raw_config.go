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
	if raw.IsNull() || !raw.IsKnown() || !raw.Type().IsObjectType() {
		return "", false, true
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
