package tui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// lineReader reads input lines with editing and history when stdin is a
// terminal (arrow keys, backspace, ctrl+a/e/u/w), and falls back to a plain
// scanner for piped input.
type lineReader struct {
	isTTY   bool
	scanner *bufio.Scanner
	history []string
}

func newLineReader() *lineReader {
	r := &lineReader{isTTY: term.IsTerminal(int(os.Stdin.Fd()))}
	if !r.isTTY {
		r.scanner = bufio.NewScanner(os.Stdin)
		r.scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	}
	return r
}

// readLine displays prompt and reads one line. Returns ok=false on EOF/ctrl+d.
func (r *lineReader) readLine(prompt string) (string, bool) {
	if !r.isTTY {
		fmt.Print(prompt)
		if !r.scanner.Scan() {
			return "", false
		}
		return r.scanner.Text(), true
	}
	line, ok := r.readRaw(prompt)
	if ok && strings.TrimSpace(line) != "" {
		if len(r.history) == 0 || r.history[len(r.history)-1] != line {
			r.history = append(r.history, line)
		}
	}
	return line, ok
}

func (r *lineReader) readRaw(prompt string) (string, bool) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		// Raw mode unavailable: degrade to scanner.
		r.isTTY = false
		r.scanner = bufio.NewScanner(os.Stdin)
		return r.readLine(prompt)
	}
	defer term.Restore(fd, old)

	var (
		buf     []rune
		cursor  int
		histPos = len(r.history)
		pending string // saves in-progress line while browsing history
	)
	redraw := func() {
		fmt.Print("\r\x1b[K" + prompt + string(buf))
		if n := len(buf) - cursor; n > 0 {
			fmt.Printf("\x1b[%dD", n)
		}
	}
	redraw()

	rd := bufio.NewReader(os.Stdin)
	for {
		c, _, err := rd.ReadRune()
		if err != nil {
			fmt.Print("\r\n")
			return "", false
		}
		switch c {
		case '\r', '\n':
			fmt.Print("\r\n")
			return string(buf), true
		case 3: // ctrl+c: clear line
			fmt.Print("^C\r\n")
			buf, cursor = nil, 0
			histPos = len(r.history)
			redraw()
		case 4: // ctrl+d: EOF on empty line
			if len(buf) == 0 {
				fmt.Print("\r\n")
				return "", false
			}
		case 127, 8: // backspace
			if cursor > 0 {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
				redraw()
			}
		case 1: // ctrl+a
			cursor = 0
			redraw()
		case 5: // ctrl+e
			cursor = len(buf)
			redraw()
		case 21: // ctrl+u: clear before cursor
			buf = append([]rune{}, buf[cursor:]...)
			cursor = 0
			redraw()
		case 23: // ctrl+w: delete word before cursor
			i := cursor
			for i > 0 && buf[i-1] == ' ' {
				i--
			}
			for i > 0 && buf[i-1] != ' ' {
				i--
			}
			buf = append(buf[:i], buf[cursor:]...)
			cursor = i
			redraw()
		case 27: // escape sequence
			b1, _ := rd.ReadByte()
			if b1 != '[' && b1 != 'O' {
				continue
			}
			b2, _ := rd.ReadByte()
			switch b2 {
			case 'A': // up: history back
				if histPos > 0 {
					if histPos == len(r.history) {
						pending = string(buf)
					}
					histPos--
					buf = []rune(r.history[histPos])
					cursor = len(buf)
					redraw()
				}
			case 'B': // down: history forward
				if histPos < len(r.history) {
					histPos++
					if histPos == len(r.history) {
						buf = []rune(pending)
					} else {
						buf = []rune(r.history[histPos])
					}
					cursor = len(buf)
					redraw()
				}
			case 'C': // right
				if cursor < len(buf) {
					cursor++
					redraw()
				}
			case 'D': // left
				if cursor > 0 {
					cursor--
					redraw()
				}
			case 'H': // home
				cursor = 0
				redraw()
			case 'F': // end
				cursor = len(buf)
				redraw()
			case '3': // delete key: ESC [ 3 ~
				rd.ReadByte() // consume '~'
				if cursor < len(buf) {
					buf = append(buf[:cursor], buf[cursor+1:]...)
					redraw()
				}
			}
		default:
			if c >= 32 { // printable
				buf = append(buf[:cursor], append([]rune{c}, buf[cursor:]...)...)
				cursor++
				redraw()
			}
		}
	}
}
