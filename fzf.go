//nolint:unused
package main

import (
	"fmt"
	"strings"
	"sync"

	fzf "github.com/junegunn/fzf/src"
)

func arg(key string, value ...string) string {
	if len(value) == 0 {
		return "--" + key
	}
	return fmt.Sprintf("--%s=%s", key, strings.Join(value, ","))
}

func multiArg() string {
	return arg("multi")
}

func printQueryArg() string {
	return arg("print-query")
}

func bindingArg(value ...string) string {
	return arg("bind", value...)
}

func previewArg(value ...string) string {
	return arg("preview", value...)
}

func previewWindowArg(value ...string) string {
	return arg("preview-window", value...)
}

func previewLabelArg(value ...string) string {
	return arg("preview-label", value...)
}

func styleArg(value ...string) string {
	return arg("style", value...)
}

func delimiterArg(value ...string) string {
	return arg("delimiter", value...)
}

func inputLabelArg(value ...string) string {
	return arg("input-label", value...)
}

func listLabelArg(value ...string) string {
	return arg("list-label", value...)
}

func headerArg(value ...string) string {
	return arg("header", value...)
}

func headerLabelArg(value ...string) string {
	return arg("header-label", value...)
}

func reverseArg() string {
	return arg("reverse")
}

func promptArg(value ...string) string {
	return arg("prompt", value...)
}

func withNthArg(value ...string) string {
	return arg("with-nth", value...)
}

func acceptNthArg(value ...string) string {
	return arg("accept-nth", value...)
}

func invokeFzf(inputChan chan string, extraFzfArgs []string) ([]string, int, error) {
	outputChan := make(chan string)

	var selected []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for s := range outputChan {
			selected = append(selected, s)
		}
	}()

	args := append([]string{
		bindingArg("ctrl-c:become:"),
		headerLabelArg("Keybindings"),
		previewWindowArg("wrap", "down:3"),
		previewLabelArg("Command Preview"),
		styleArg("full"),
		delimiterArg(fieldDelimiter),
	}, extraFzfArgs...)

	options, err := fzf.ParseOptions(true, args)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse fzf options: %w", err)
	}

	options.Input = inputChan
	options.Output = outputChan

	rc, err := fzf.Run(options)

	// fzf unfortunately doesn't seem to close output channel itself, so we have to do it for the goroutine to finish
	close(outputChan)
	wg.Wait()

	return selected, rc, err
}
