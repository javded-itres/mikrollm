package promptcache

import "strings"

const (
	ModeOff     = "off"
	ModeAuto    = "auto"
	ModeOn      = "on"
	ModeInherit = "inherit"
)

func NormalizeMode(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func ValidAliasMode(s string) bool {
	switch NormalizeMode(s) {
	case ModeInherit, ModeOff, ModeAuto, ModeOn:
		return true
	default:
		return false
	}
}

func ValidGlobalMode(s string) bool {
	switch NormalizeMode(s) {
	case ModeOff, ModeAuto, ModeOn:
		return true
	default:
		return false
	}
}

func Resolve(global, alias string) string {
	a := NormalizeMode(alias)
	switch a {
	case ModeOff, ModeAuto, ModeOn:
		return a
	}
	g := NormalizeMode(global)
	if ValidGlobalMode(g) {
		return g
	}
	return ModeAuto
}
