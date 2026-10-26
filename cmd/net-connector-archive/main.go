package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gatework/go-net-connector/archive"
	"github.com/gatework/go-net-connector/internal/buildinfo"
	"golang.org/x/sys/unix"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("net-connector-archive protocol %d; %s\n", archive.Version, buildinfo.Identity)
		return
	}
	if len(os.Args) != 1 {
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	input, err := pollableStandardFile(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	defer input.Close()
	output, err := pollableStandardFile(os.Stdout)
	if err != nil {
		os.Exit(1)
	}
	defer output.Close()
	if archive.Serve(ctx, input, output) != nil {
		os.Exit(1)
	}
}

func pollableStandardFile(file *os.File) (*os.File, error) {
	fd, err := unix.Dup(int(file.Fd()))
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	if err = unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "archive-protocol"), nil
}
