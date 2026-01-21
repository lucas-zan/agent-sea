package ui

import (
	"fmt"
	"strings"
)

// IsRawMode indicates if the terminal is currently in raw mode.
// This is a simple global switch for the CLI.
var IsRawMode = false

// Printf mimics fmt.Printf but handles CRLF if in raw mode.
func Printf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	Print(s)
}

// Print mimics fmt.Print but handles CRLF if in raw mode.
func Print(a ...interface{}) {
	s := fmt.Sprint(a...)
	if IsRawMode {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	fmt.Print(s)
}

// Println mimics fmt.Println but handles CRLF if in raw mode.
func Println(a ...interface{}) {
	s := fmt.Sprint(a...)
	if IsRawMode {
		s = strings.ReplaceAll(s, "\n", "\r\n")
		// fmt.Println adds a newline at the end, we need to make sure that one is also CRLF'd
		fmt.Print(s + "\r\n")
	} else {
		fmt.Println(s)
	}
}
