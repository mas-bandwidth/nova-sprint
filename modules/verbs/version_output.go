package verbs

// versionPrefix starts the version verb's one line: "nova-sprint <version>".
const versionPrefix = "nova-sprint "

// VersionLine is the version verb's line for version.
func VersionLine(version string) string { return versionPrefix + orWord(version, "(unknown build)") }
