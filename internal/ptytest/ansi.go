package ptytest

import (
	"regexp"
	"strconv"
	"strings"
)

const ansiClearScreen = "\x1b[2J"

var lineClearAtCol1Re = regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[(?:[0-2])?K`)

// HasFullRedrawANSI reports whether output includes a full-screen redraw.
func HasFullRedrawANSI(data string, rows int) bool {
	if strings.Contains(data, ansiClearScreen) {
		return true
	}
	if rows <= 0 {
		return false
	}
	rowsSeen := make(map[int]struct{})
	matches := lineClearAtCol1Re.FindAllStringSubmatch(data, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		row, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		rowsSeen[row] = struct{}{}
	}
	return len(rowsSeen) >= rows-1
}
