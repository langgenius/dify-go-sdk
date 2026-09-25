package dify

import (
	"runtime"
	"runtime/debug"
	"sync"
)

// modulePath is this SDK's import path, which is how the build info names it.
const modulePath = "github.com/langgenius/dify-go-sdk"

// Version is what this SDK reports itself as, read from the build info of the
// program it is compiled into rather than written down here, where it would
// drift from the tag. It is "(devel)" when built from a checkout that is the
// main module, since there is no tag to read.
func Version() string {
	versionOnce.Do(func() {
		version = "(devel)"
		info, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		if info.Main.Path == modulePath && info.Main.Version != "" {
			version = info.Main.Version
			return
		}
		for _, dep := range info.Deps {
			if dep.Path == modulePath {
				if dep.Replace != nil && dep.Replace.Version != "" {
					version = dep.Replace.Version
				} else if dep.Version != "" {
					version = dep.Version
				}
				return
			}
		}
	})
	return version
}

var (
	versionOnce sync.Once
	version     string
)

// UserAgent is what this SDK sends. Dify logs the User-Agent of everything
// that talks to it; Go's default says only "Go-http-client/1.1", which tells
// an operator looking at a misbehaving client nothing about which SDK or
// version it was.
func UserAgent() string {
	return "dify-go-sdk/" + Version() + " " + runtime.Version()
}
