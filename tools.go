//go:build tools

package tools

// Keeps golang.org/x/mobile in the module graph for `gomobile bind`.
import _ "golang.org/x/mobile/bind"
