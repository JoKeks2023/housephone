// Package version holds the build version (set via -ldflags).
package version

// Version is overridden at build time:
//
//	go build -ldflags "-X github.com/JoKeks2023/housephone/bridge/internal/version.Version=1.2.3"
var Version = "dev"
