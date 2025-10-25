package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	fzf "github.com/junegunn/fzf/src"
)

const (
	fieldDelimiter = "\t"
)

var (
	varPatternRegex = regexp.MustCompile(`{{\s*([\w-]+)\s*}}`)
)

func toChan[T any](s []T) chan T {
	ch := make(chan T, len(s))
	defer close(ch)
	for _, e := range s {
		ch <- e
	}
	return ch
}

func formatLine(fields ...string) string {
	return strings.Join(fields, fieldDelimiter)
}

func placeholderRegex(varName string) *regexp.Regexp {
	return regexp.MustCompile(`{{\s*` + regexp.QuoteMeta(varName) + `\s*}}`)
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
	if err = cmd.Wait(); err != nil {
		log.Printf("command '%q' finished with error: %v\n", cmdStr, err)
	}
}

func replaceVariables(template string, vars map[string]VarDefinition) (string, error) {
	varPattern := regexp.MustCompile(`{{\s*(.*?)\s*}}`)
	variables := varPattern.FindAllStringSubmatch(template, -1)

	seen := make(map[string]bool)
	result := template

	fzfArgs := []string{
		inputLabelArg("Ctrl-C: Quit | Esc: Skip"),
	}

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

		val, err := promptVariable(varName, varDef, result, fzfArgs)
		if err != nil {
			return "", fmt.Errorf("failed to read variable %q: %w", varName, err)
		}

		re := placeholderRegex(varName)
		result = re.ReplaceAllString(result, val)
	}

	return result, nil
}

func promptVariable(varName string, varDef *VarDefinition, currentCommand string, fzfArgs []string) (string, error) {
	var err error
	options := make(chan string)
	delimiter := " "
	var label string
	var preview string

	escapedCurrentCommand := strings.ReplaceAll(currentCommand, `'`, `\'`)
	if varDef == nil {
		close(options)

		fzfArgs = append(fzfArgs, printQueryArg())
		label = fmt.Sprintf("Enter {{%s}}", varName)
		preview = fmt.Sprintf(
			`[ -z {q} ] && echo "%[1]s" || echo '%[1]s' | sed -E "s|\\{\\{\\s*%s\\s*\\}\\}|$(printf "%%s" {q})|g"`,
			escapedCurrentCommand, varName,
		)
	} else {
		label = fmt.Sprintf("Select %s", varName)
		preview = fmt.Sprintf(
			`[ -z {+} ] && echo '%[1]s' || echo '%[1]s' | sed -E "s|\\{\\{\\s*%s\\s*\\}\\}|$(printf "%%s\n" {+} | paste -sd '%s')|g"`,
			escapedCurrentCommand, varName, delimiter,
		)
		delimiter = varDef.Delimiter
		if varDef.Multi {
			fzfArgs = append(fzfArgs, multiArg())
		}

		if len(varDef.Options) > 0 {
			options = toChan(varDef.Options)
		} else if varDef.OptionsCmd != "" {
			go executeCommand(varDef.OptionsCmd, options)
		}
	}
	fzfArgs = append(fzfArgs, promptArg(label+": "), previewArg(preview))

	selected, _, err := invokeFzf(options, fzfArgs)

	if err != nil {
		return "", fmt.Errorf("failed to select variable %q: %w", varName, err)
	}
	return strings.Join(selected, delimiter), nil
}

func selectCommand(cmds []Command) (*Command, error) {
	input := make(chan string, len(cmds))
	go func() {
		defer close(input)
		for index, cmd := range cmds {
			inputLabel := []string{"Ctrl-C: Quit"}
			executeAction := "ignore"

			if !varPatternRegex.MatchString(cmd.Cmd) {
				inputLabel = append(inputLabel, "Ctrl-E: Execute")
				executeAction = "become(sh -c {3})"
			}

			// Cmd Index | Cmd Title | Cmd | Keybinds | Execute Action
			input <- formatLine(strconv.Itoa(index), cmd.Title, cmd.Cmd,
				strings.Join(inputLabel, " | "), executeAction)
		}
	}()

	args := []string{
		withNthArg("{2}"),
		acceptNthArg("{1}"),
		previewArg("echo {3}"),
		bindingArg("focus:transform-input-label:echo {4}"),
		bindingArg("ctrl-e:transform:echo {5}"),
		//inputLabelArg("Select Command"),
		listLabelArg("Commands"),
		inputLabelArg("Ctrl-C: Quit"),
	}
	selection, rc, err := invokeFzf(input, args)
	if err != nil {
		return nil, fmt.Errorf("command selection failed: %w", err)
	}

	if len(selection) == 0 || rc == fzf.ExitInterrupt {
		return nil, nil
	}

	commandIndex, err := strconv.Atoi(selection[0])
	if err != nil {
		return nil, err
	}
	if commandIndex >= len(cmds) {
		return nil, fmt.Errorf("command index not found: %v", commandIndex)
	}

	cmd := cmds[commandIndex]
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
