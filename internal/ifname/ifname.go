// Package ifname provides comparison keys, display forms, and command names
// for network device interfaces.
package ifname

import (
	"regexp"
	"strings"
)

type interfaceRule struct{ pattern, canonical, short string }

var interfaceRules = []interfaceRule{
	{`bridge-aggregation|bagg`, "bridge-aggregation", "BAGG"}, {`route-aggregation|ragg`, "route-aggregation", "RAGG"}, {`aggregate|agg`, "aggregate", "Agg"},
	{`hundredgigabitethernet|hundredgige|hge|100ge|hu`, "hundredgigabitethernet", "Hu"}, {`fortygigabitethernet|fortygige|fge|40ge|fo`, "fortygigabitethernet", "Fo"},
	{`twenty-?fivegigabitethernet|twenty-?fivegige|wge|25ge|twe`, "twentyfivegigabitethernet", "Twe"},
	{`xge|10ge|xgigabitethernet|ten-gigabitethernet|tengigabitethernet|ten-?gige|xethernet|xe|te`, "tengigabitethernet", "Te"},
	{`smartrate-ethernet|se`, "smartrate-ethernet", "SE"}, {`gigabitethernet|ge|gi|g`, "gigabitethernet", "Gi"}, {`fastethernet|fa`, "fastethernet", "Fa"},
	{`ethernet|eth`, "ethernet", "Eth"}, {`port-channel|po`, "port-channel", "Po"},
}

var interfacePatterns = func() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(interfaceRules))
	for i, rule := range interfaceRules {
		patterns[i] = regexp.MustCompile(`(?i)^(?:` + rule.pattern + `)([0-9])`)
	}
	return patterns
}()

// Canonical returns a comparison key and whether the name matched a known family.
func Canonical(name string) (string, bool) {
	for i, pattern := range interfacePatterns {
		if match := pattern.FindStringIndex(name); match != nil {
			return strings.ToLower(interfaceRules[i].canonical + name[match[1]-1:]), true
		}
	}
	return strings.ToLower(name), false
}

// Key returns a case-insensitive comparison key for an interface name.
func Key(name string) string {
	key, _ := Canonical(name)
	return key
}

// Short returns the conventional display form for known interface families.
func Short(name string) (string, bool) {
	for i, pattern := range interfacePatterns {
		if match := pattern.FindStringIndex(name); match != nil {
			return interfaceRules[i].short + name[match[1]-1:], true
		}
	}
	return name, false
}

type configurationRule struct {
	pattern *regexp.Regexp
	name    string
}

var configurationRules = []configurationRule{
	{regexp.MustCompile(`(?i)^(?:se)([0-9])`), "Smartrate-Ethernet"},
	{regexp.MustCompile(`(?i)^(?:xge)([0-9])`), "Ten-GigabitEthernet"},
	{regexp.MustCompile(`(?i)^(?:ge|gi|g)([0-9])`), "GigabitEthernet"},
	{regexp.MustCompile(`(?i)^(?:te)([0-9])`), "TenGigabitEthernet"},
	{regexp.MustCompile(`(?i)^(?:fa)([0-9])`), "FastEthernet"},
	{regexp.MustCompile(`(?i)^(?:eth)([0-9])`), "Ethernet"},
	{regexp.MustCompile(`(?i)^(?:po)([0-9])`), "port-channel"},
}

// ConfigName expands abbreviated interface names for device configuration commands.
func ConfigName(name string) string {
	for _, rule := range configurationRules {
		if match := rule.pattern.FindStringIndex(name); match != nil {
			return rule.name + name[match[1]-1:]
		}
	}
	return name
}
