package ctlcmd

import "errors"

const (
	exitSuccess        = 0
	exitGeneral        = 1
	exitUsage          = 2
	exitAuthentication = 3
	exitConnection     = 4
	exitServerRejected = 5
	exitCompatibility  = 6
)

type commandError struct {
	kind    int
	message string
}

func (e *commandError) Error() string { return e.message }

func usageError(message string) error    { return &commandError{kind: exitUsage, message: message} }
func internalError(message string) error { return &commandError{kind: exitGeneral, message: message} }
func compatibilityError(message string) error {
	return &commandError{kind: exitCompatibility, message: message}
}

func commandExitCode(err error) int {
	if err == nil {
		return exitSuccess
	}
	var target *commandError
	if errors.As(err, &target) {
		return target.kind
	}
	return exitGeneral
}
