package arctic

import (
	"log"
	"os"
)

// logVerbose gates the package's internal progress logging. The CLI flips it on
// when the user did not ask for quiet output. By default the library stays
// silent so it can be embedded without spraying a host program's logs.
var logVerbose = os.Getenv("ARCTIC_VERBOSE") != ""

// SetVerbose turns the package's internal logging on or off.
func SetVerbose(v bool) { logVerbose = v }

func logf(format string, args ...any) {
	if logVerbose {
		log.Printf(format, args...)
	}
}
