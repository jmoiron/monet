package passwd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"
)

// entirely based on MIT code from Joe Linoff
// https://gist.github.com/jlinoff/e8e26b4ffa38d379c7f1891fd174a6d0
// edited for style

func GetPassword(prompt string) (string, error) {
	// Get the initial state of the terminal.
	state, err := terminal.GetState(syscall.Stdin)
	if err != nil {
		return "", err
	}

	// Restore it in the event of an interrupt.
	// CITATION: Konstantin Shaposhnikov - https://groups.google.com/forum/#!topic/golang-nuts/kTVAbtee9UA
	c := make(chan os.Signal)
	cancel := make(chan struct{})
	signal.Notify(c, os.Interrupt, os.Kill)
	go func() {
		select {
		case <-c:
			_ = terminal.Restore(syscall.Stdin, state)
			os.Exit(1)
		case <-cancel:
		}
	}()

	// prompt the user for the password
	fmt.Println(prompt)
	p, err := terminal.ReadPassword(syscall.Stdin)
	fmt.Println("")

	// stop looking for ^C on the channel.
	signal.Stop(c)
	// close the waiting goroutine
	close(cancel)

	if err != nil {
		return "", err
	}

	// Return the password as a string.
	return string(p), nil
}
