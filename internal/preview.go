package internal

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

func newPreviewCommand() *cli.Command {
	return &cli.Command{
		Name:      "preview",
		Usage:     "Generate preview output for fzf",
		UsageText: "cmdmark preview [command options] [selections...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "template", Usage: "Template string containing {{varName}} placeholders"},
			&cli.StringFlag{Name: "varName", Usage: "Variable name to replace"},
			&cli.BoolFlag{Name: "required", Usage: "Whether a value is required"},
			&cli.BoolFlag{Name: "allowFreeform", Usage: "Allow freeform input if no match"},
			&cli.StringFlag{Name: "delimiter", Usage: "Delimiter for multi-selection", Value: ""},
			&cli.StringFlag{Name: "query", Usage: "fzf {q} query", Value: ""},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			template := c.String("template")
			varName := c.String("varName")
			required := c.Bool("required")
			allowFreeform := c.Bool("allowFreeform")
			delimiter := c.String("delimiter")
			query := c.String("query")

			selections := c.Args().Slice()

			//fmt.Printf("selections: %v\tdelimiter: %v\ttemplate:%v\tquery:%v\n",
			//	len(selections), delimiter, template, len(query))

			var value string

			switch {
			case len(selections) > 0:
				if delimiter != "" {
					value = strings.Join(selections, delimiter)
				} else {
					value = strings.Join(selections, " ")
				}

			case allowFreeform && query != "":
				value = query

			case len(selections) == 0 && query == "":
				fmt.Println(template)
				return nil

			default:
				if required {
					fmt.Println("No match and freeform input is not allowed.")
					os.Exit(1)
				}
				fmt.Println(template)
				return nil
			}

			fmt.Println(replacePlaceholder(template, varName, value))
			return nil
		},
	}
}
