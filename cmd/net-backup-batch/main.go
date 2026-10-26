package main

import (
	"context"
	"github.com/gatework/go-net-connector/internal/cli"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit((cli.Runtime{Args: os.Args[1:], Program: os.Args[0]}).Run(ctx, "net-backup-batch"))
}
