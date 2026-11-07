package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gatework/go-net-connector/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := (cli.Runtime{Args: os.Args[1:], Input: os.Stdin, Output: os.Stdout, Error: os.Stderr, Program: os.Args[0]}).Run(ctx)
	stop()
	os.Exit(code)
}
