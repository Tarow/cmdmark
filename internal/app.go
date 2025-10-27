package internal

import "github.com/urfave/cli/v3"

func NewApp() *cli.Command {
	return &cli.Command{
		Name:  "cmdmark",
		Usage: "Manage and preview command templates",
		Commands: []*cli.Command{
			newSearchCommand(),
			newPreviewCommand(),
		},
	}
}
