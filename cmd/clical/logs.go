package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/miphe/clical/internal/app"
)

// LogsCmd prints the end of clical's log: sync failures and recoveries,
// and calendar and account changes.
type LogsCmd struct {
	Lines  int  `short:"n" default:"50" help:"How many of the last lines to print."`
	Follow bool `short:"f" help:"Keep printing new lines as they're written (ctrl+c stops)."`
}

func (c *LogsCmd) Run() error {
	path := app.DefaultPaths().Log
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !c.Follow {
		fmt.Printf("Nothing logged yet (%s).\n", path)
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	os.Stdout.Write(lastLines(data, c.Lines))
	if !c.Follow {
		return nil
	}
	return follow(path, int64(len(data)), os.Stdout)
}

// lastLines is the last n lines of data.
func lastLines(data []byte, n int) []byte {
	if n <= 0 {
		return nil
	}
	end := len(data)
	if end > 0 && data[end-1] == '\n' {
		end-- // the final newline doesn't start a line
	}
	for i := end - 1; i >= 0; i-- {
		if data[i] == '\n' {
			n--
			if n == 0 {
				return data[i+1:]
			}
		}
	}
	return data
}

// follow prints what's added to path from offset on, starting over when the
// log is rotated (it gets shorter).
func follow(path string, offset int64, w io.Writer) error {
	for {
		time.Sleep(500 * time.Millisecond)
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		fi, err := f.Stat()
		if err == nil && fi.Size() < offset {
			offset = 0
		}
		if err == nil && fi.Size() > offset {
			var buf bytes.Buffer
			if _, err := f.Seek(offset, io.SeekStart); err == nil {
				n, _ := io.Copy(&buf, f)
				offset += n
				w.Write(buf.Bytes())
			}
		}
		f.Close()
	}
}
