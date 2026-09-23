//go:build windows

package releaseverify

import (
	"os"
	"os/exec"
	"time"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = time.Second
}
