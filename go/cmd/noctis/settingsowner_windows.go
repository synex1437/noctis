//go:build windows

package main

// ownsLocalSettingsRoot is false on Windows, where Claude Code 2.1.289 does not tell who owns a directory and
// keeps a project's local settings in the directory a session started in.
func ownsLocalSettingsRoot(string) bool {
	return false
}
