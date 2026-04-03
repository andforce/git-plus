package main

import (
	"log"

	"github.com/ImSingee/git-plus/pkg/app"
)

var version = "dev"

func main() {
	if err := app.NewRootCommand(version, newFrontendHandler).Execute(); err != nil {
		log.Fatal(err)
	}
}
