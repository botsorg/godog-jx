package main

import (
	"os"

	"github.com/jenkins-x/jx/cmd/jx/app"
)

func main() {
	if err := app.Run(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
