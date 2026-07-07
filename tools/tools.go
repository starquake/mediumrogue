//go:build tools

// Package tools pins the versions of Go-built CLI tools this repo uses, so
// dependabot's gomod ecosystem tracks them (the topbanana pattern). The
// `tools` build tag keeps these imports out of normal builds; the Makefile
// builds the binaries out of this module into build/bin.
package tools

import (
	_ "github.com/gzuidhof/tygo"
)
