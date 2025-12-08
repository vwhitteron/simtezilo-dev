package app

var (
	Version   = "DEV" //nolint:gochecknoglobals // special global for build info
	BuildTime string  //nolint:gochecknoglobals // special global for build info
)

type Info struct {
	BuildTime string
	Version   string
}
