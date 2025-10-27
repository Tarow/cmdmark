package main

import (
	"context"
	"log"
	"os"

	"github.com/tarow/cmdmark/internal"
)

func main() {
	if err := internal.NewApp().Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
