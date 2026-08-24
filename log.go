package nya

// Log, when set, receives progress messages from Repair and the recovery
// volume helpers. It is nil by default so that the package never writes to
// standard output on its own.
var Log func(format string, args ...any)

func logf(format string, args ...any) {
	if Log != nil {
		Log(format, args...)
	}
}
