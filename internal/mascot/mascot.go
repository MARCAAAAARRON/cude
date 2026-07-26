package mascot

import "strings"

const Raw = `|-- --|
_-_| |||_-
| |_---| |`

func Lines() []string {
	return strings.Split(Raw, "\n")
}
