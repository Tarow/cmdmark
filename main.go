package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	fzf "github.com/junegunn/fzf/src"
)

const (
	fieldDelimiter = "\t"
)

func toChan[T any](s []T) chan T {
	ch := make(chan T, len(s))
	defer close(ch)
	for _, e := range s {
		ch <- e
	}
	return ch
}

func buildCommandMap(cmds []Command) map[string]Command {
	cmdMap := make(map[string]Command, len(cmds))
	for _, cmd := range cmds {
		cmdMap[cmd.Title] = cmd
	}
	return cmdMap
}

func formatLine(fields ...string) string {
	return strings.Join(fields, fieldDelimiter)
}

func placeholderRegex(varName string) *regexp.Regexp {
	return regexp.MustCompile(`{{\s*` + regexp.QuoteMeta(varName) + `\s*}}`)
}

// Runs fzf. Extra options can be set by providing FZF_DEFAULT_OPTS environment variable.
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
		"--bind=ctrl-c:become:",
		"--preview-window=wrap,up:3",
		"--bind=F2:toggle-preview",
		"--preview-label=Command Preview",
		"--style=full",
		fmt.Sprintf("--delimiter=%s", fieldDelimiter),
	}, extraFzfArgs...)

	options, err := fzf.ParseOptions(true, args)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to parse fzf options: %w", err)
	}

	options.Input = inputChan
	options.Output = outputChan

	rc, err := fzf.Run(options)

	close(outputChan)

	wg.Wait()

	return selected, rc, err
}

func executeCommand(cmdStr string, output chan string) {
	defer close(output)
	cmd := exec.Command("sh", "-c", cmdStr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("failed to get stdout pipe: %v\n", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("failed to start command %q: %v\n", cmdStr, err)
		return
	}

	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			output <- line
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("reading command output %q: %v\n", cmdStr, err)
	}
	cmd.Wait()
}

func promptVariable(varName string, varDef *VarDefinition, currentCommand string) (string, error) {
	var err error

	delimiter := " "
	label := fmt.Sprintf("Enter value for {{%s}}", varName)
	if varDef != nil {
		if varDef.Multi {
			delimiter = varDef.Delimiter
		}
		label = fmt.Sprintf("Select %s", varName)
	}

	fzfArgs := []string{
		fmt.Sprintf("--input-label=%s", label),
	}
	if varDef != nil && varDef.Multi {
		fzfArgs = append(fzfArgs, "--multi")
	}

	preview := fmt.Sprintf(
		`--preview=echo '%s' | sed -E "s|\\{\\{\\s*%s\\s*\\}\\}|$(printf "%%s\n" {+} | paste -sd '%s')|g"`,
		strings.ReplaceAll(currentCommand, `'`, `\'`), varName, delimiter,
	)

	options := make(chan string)
	if varDef == nil {
		close(options)
		// Free-form variables with no definition
		fzfArgs = append(fzfArgs, "--print-query")
		preview = fmt.Sprintf(
			`--preview=echo '%s' | sed -E "s|\\{\\{\\s*%s\\s*\\}\\}|$(printf "%%s" {q})|g"`,
			strings.ReplaceAll(currentCommand, `'`, `\'`), varName,
		)
	} else if len(varDef.Options) > 0 {
		// Predefined options
		options = toChan(varDef.Options)
	} else if varDef.OptionsCmd != "" {
		// Options from command
		go executeCommand(varDef.OptionsCmd, options)
	}
	fzfArgs = append(fzfArgs, preview)

	selected, _, err := invokeFzf(options, fzfArgs)

	if err != nil {
		return "", fmt.Errorf("failed to select variable %q: %w", varName, err)
	}
	return strings.Join(selected, delimiter), nil
}

func replaceVariables(template string, vars map[string]VarDefinition) (string, error) {
	varPattern := regexp.MustCompile(`{{\s*(.*?)\s*}}`)
	variables := varPattern.FindAllStringSubmatch(template, -1)

	seen := make(map[string]bool)
	result := template

	for _, v := range variables {
		if len(v) < 2 {
			continue
		}

		varName := strings.TrimSpace(v[1])
		if seen[varName] {
			continue
		}
		seen[varName] = true

		var varDef *VarDefinition
		if def, exists := vars[varName]; exists {
			varDef = &def
		}

		val, err := promptVariable(varName, varDef, result)
		if err != nil {
			return "", fmt.Errorf("failed to read variable %q: %w", varName, err)
		}

		re := placeholderRegex(varName)
		result = re.ReplaceAllString(result, val)
	}

	return result, nil
}

func selectCommand(cmds []Command) (*Command, error) {
	input := make(chan string, len(cmds))
	go func() {
		defer close(input)
		for _, cmd := range cmds {
			input <- formatLine(cmd.Title, cmd.Cmd)
		}
	}()

	args := []string{
		"--with-nth=1",
		"--preview=echo {2}",
		"--input-label=Select Command",
		"--list-label=Commands",
	}
	selectedLines, rc, err := invokeFzf(input, args)
	if err != nil {
		return nil, fmt.Errorf("command selection failed: %w", err)
	}

	if len(selectedLines) == 0 || rc == fzf.ExitInterrupt {
		return nil, nil
	}

	parts := strings.Split(selectedLines[0], fieldDelimiter)
	selectedID := parts[0]

	cmdMap := buildCommandMap(cmds)
	cmd, exists := cmdMap[selectedID]
	if !exists {
		return nil, fmt.Errorf("command not found: %s", selectedID)
	}

	return &cmd, nil
}

func run() error {
	var config Config
	var err error

	if len(os.Args) > 1 {
		config, err = loadConfig(os.Args[1])
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	} else {
		return errors.New("no config file provided")
	}

	cmd, err := selectCommand(config.Commands)
	if err != nil || cmd == nil {
		return err
	}

	mergedVars := mergeVars(config.GlobalVars, cmd.Vars)

	fullCmd, err := replaceVariables(cmd.Cmd, mergedVars)
	if err != nil {
		return err
	}
	fmt.Println(fullCmd)

	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
}
