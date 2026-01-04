// Package shared contains web UI assets shared between run mode and setup mode
package common

import (
	"embed"
)

//go:embed static/*
var StaticFiles embed.FS
