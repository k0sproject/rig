package sshconfig

import (
	"bufio"
	"strings"
)

// defaultListAliases maps the deprecated names some of the default configs
// still use to the current ones.
var defaultListAliases = map[string]string{
	"hostbasedkeytypes":      "hostbasedacceptedalgorithms",
	"pubkeyacceptedkeytypes": "pubkeyacceptedalgorithms",
}

// defaultList returns the default list of algorithms for key, which the +, -
// and ^ modifiers apply to. Like ssh, they modify the client's defaults, and
// rig's are the ones its built-in default config sets: the algorithms the
// local ssh binary supports say nothing about rig, which does not use it. key
// is matched case-insensitively, through the deprecated aliases too.
func defaultList(key string) string {
	key = canonicalListKey(key)
	scanner := bufio.NewScanner(strings.NewReader(sshDefaultConfig))
	for scanner.Scan() {
		name, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		if ok && canonicalListKey(name) == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func canonicalListKey(key string) string {
	key = strings.ToLower(key)
	if current, ok := defaultListAliases[key]; ok {
		return current
	}
	return key
}
