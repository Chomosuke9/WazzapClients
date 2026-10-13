package desktop

// SystemDark reports whether the system asks apps for a dark theme. ok is
// false when the system doesn't say (or this one can't be asked).
func SystemDark() (dark, ok bool) { return systemDark() }
