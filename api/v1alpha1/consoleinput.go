package v1alpha1

import "regexp"

// DefaultConsoleInput is where console lines go when the Egg declares no ConsoleInput: the stdin of
// the container's PID 1, which is the game process because the start command runs under exec.
const DefaultConsoleInput = "/proc/1/fd/0"

// consoleInputPattern keeps the path free of anything a shell would interpret, so it may also be
// embedded in a script (the preStop hook).
var consoleInputPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,255}$`)

// ConsoleInputPath is the single resolution of where console lines are written. A path Validate
// would refuse (an Egg that skipped the webhook) falls back to the default.
func (s *EggSpec) ConsoleInputPath() string {
	if consoleInputPattern.MatchString(s.ConsoleInput) {
		return s.ConsoleInput
	}
	return DefaultConsoleInput
}

// ConsoleWriteScript writes the line $LINE to the file $TARGET (both expanded by the shell, never
// interpolated). It opens the target without O_CREAT first: a named pipe owned by the game's user in
// a sticky /tmp cannot be opened with O_CREAT by root when the kernel's fs.protected_fifos is on
func ConsoleWriteScript(target, line string) string {
	return `printf '%s\n' "` + line + `" | dd of="` + target + `" conv=nocreat,notrunc status=none 2>/dev/null || printf '%s\n' "` + line + `" > "` + target + `"`
}

// ConsoleLineCommand builds the exec command that types one line into the game's console. The path
// and the line are arguments, never part of the script: nothing in them is interpreted.
func ConsoleLineCommand(path, line string) []string {
	return []string{"sh", "-c", ConsoleWriteScript("$1", "$2"), "sh", path, line}
}
