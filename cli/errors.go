package cli

import (
	"errors"
	"fmt"

	"github.com/tamnd/arctic-cli/arctic"
	"github.com/tamnd/arctic-cli/shift"
)

func isBlocked(err error) bool {
	return errors.Is(err, shift.ErrBlocked)
}

func isNotPublished(err error) bool {
	var np *arctic.ErrNotPublished
	return errors.As(err, &np)
}

func isStall(err error) bool {
	return errors.Is(err, arctic.ErrCommitStall)
}

// mapErr converts a library error into the right exit code.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case isStall(err):
		return codeError(exitStall, fmt.Errorf("%w\nrestart the command to resume at this month", err))
	case isBlocked(err):
		return codeError(exitBlocked, fmt.Errorf("%w\nhint: slow down, lower --workers, or wait", err))
	case isNotPublished(err):
		return codeError(exitNoData, err)
	default:
		return codeError(exitError, err)
	}
}
