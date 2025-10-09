package app

var Version = "DEV"  //nolint:gochecknoglobals // special global for build info
var BuildTime string //nolint:gochecknoglobals // special global for build info

type Info struct {
	BuildTime string
	Version   string
}
