package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gatework/go-net-connector/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit((cli.Runtime{Args: os.Args[1:], Program: os.Args[0]}).Run(ctx, "net-backup-batch"))
}
