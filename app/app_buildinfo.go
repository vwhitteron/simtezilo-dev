package app

var (
	Version   = "dev"  //nolint:gochecknoglobals // special global for build version
	BuildTime string   //nolint:gochecknoglobals // special global for build time
	Platform  = "none" //nolint:gochecknoglobals // special global for build platform
)

type Info struct {
	BuildTime string
	Version   string
	Platform  string
}
