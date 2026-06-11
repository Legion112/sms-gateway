package main

import (
	"os"

	"github.com/legion/sms-gateway/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
