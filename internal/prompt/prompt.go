// Package prompt provides a small testable abstraction over the process terminal.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/cockroachdb/errors"
)

// Terminal combines line-oriented input and output used by interactive command
// confirmations. A terminal created with New does not own its streams; one
// created with Open owns /dev/tty and must be closed.
type Terminal struct {
	reader *bufio.Reader
	writer io.Writer
	closer io.Closer
}

// New creates a terminal facade over caller-owned streams.
func New(reader io.Reader, writer io.Writer) *Terminal {
	return &Terminal{reader: bufio.NewReader(reader), writer: writer}
}

// Open opens the controlling terminal independently of redirected stdin/stdout.
func Open() (*Terminal, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("interactive confirmation requires a terminal")
	}

	terminal := New(tty, tty)
	terminal.closer = tty

	return terminal, nil
}

// ReadLine writes question and returns one input line.
func (terminal *Terminal) ReadLine(question string) (string, error) {
	if _, err := fmt.Fprint(terminal.writer, question); err != nil {
		return "", errors.Wrap(err, "write prompt")
	}

	answer, err := terminal.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", errors.Wrap(err, "read prompt")
	}

	return answer, nil
}

// Writef writes additional interactive output.
func (terminal *Terminal) Writef(format string, args ...any) error {
	_, err := fmt.Fprintf(terminal.writer, format, args...)
	return errors.Wrap(err, "write prompt output")
}

// Close releases an owned controlling terminal.
func (terminal *Terminal) Close() error {
	if terminal.closer == nil {
		return nil
	}

	return errors.Wrap(terminal.closer.Close(), "close terminal")
}
