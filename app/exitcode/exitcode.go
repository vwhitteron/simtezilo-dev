// Package exitcode defines exit codes used by the application.
package exitcode

type ExitCode int

const (
	Success        ExitCode = 0
	GeneralFailure ExitCode = 1
	SetupMode      ExitCode = 33
)
