package internal

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	fzf "github.com/junegunn/fzf/src"
	"github.com/urfave/cli/v3"
)

func newSearchCommand() *cli.Command {
	return &cli.Command{
		Name:  "search",
		Usage: "Search commands in a config file",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "Path to config file (default: ~/.config/cmdmark/config.yaml or /etc/cmdmark/config.yaml)",
				Aliases: []string{"c"},
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			configPath := c.String("config")

			if configPath == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to get home directory: %w", err)
				}
				defaultPaths := []string{
					filepath.Join(home, ".config", "cmdmark", "config.yaml"),
					"/etc/cmdmark/config.yaml",
				}
				for _, path := range defaultPaths {
					if _, err := os.Stat(path); err == nil {
						configPath = path
						break
					}
				}
				if configPath == "" {
					return fmt.Errorf("no config file found in default locations")
				}
			}

			config, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			return runSearch(config)
		},
	}
}

var (
	varPatternRegex = regexp.MustCompile(`{{\s*([\w-]+)\s*}}`)
)

func ptr[T any](v T) *T {
	return &v
}

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

func replaceVariables(template string, varNames []string, vars map[string]VarDefinition) (string, error) {
	result := template

	for idx, varName := range varNames {
		varDef, exists := vars[varName]
		if !exists {
			// No var definition given -> It's a required freeform variable then
			varDef = VarDefinition{
				Required:      ptr(true),
				AllowFreeform: ptr(true),
				Multi:         ptr(false),
				Delimiter:     ptr(""),
			}
		}

		val, err := promptVariable(varName, varDef, result, idx == len(varNames)-1)
		if err != nil {
			return "", fmt.Errorf("failed to read variable %q: %w", varName, err)
		}

		re := placeholderRegex(varName)
		result = re.ReplaceAllString(result, val)
	}

	return result, nil
}

func valuePresentCheck(varDef VarDefinition) string {
	valuePlaceholder := ""
	valuePresentCheck := ""

	if *varDef.Multi {
		valuePlaceholder = "{+}"
	} else {
		valuePlaceholder = "{}"
	}
	valuePresentCheck = fmt.Sprintf("[ -n $(echo %v) ]", valuePlaceholder)
	if *varDef.AllowFreeform {
		valuePresentCheck += " || [ -n $(echo {q}) ]"
	}

	return valuePresentCheck
}

func promptVariable(varName string, varDef VarDefinition, currentCommand string, isLast bool) (string, error) {
	var err error
	options := make(chan string)
	delimiter := *varDef.Delimiter
	var prompt string
	var preview string
	var fzfArgs []string

	// Check condition to see if value is present. Necessary for certain binds, e.g. if direct execution is possible
	valuePresentCheck := valuePresentCheck(varDef)

	keybindings := []string{"Ctrl-C: Quit"}

	isRequired := *varDef.Required
	allowFreeform := *varDef.AllowFreeform

	fmt.Println(currentCommand)
	preview = fmt.Sprintf(
		`cmdmark preview --template %q --varName %q --required=%t --allowFreeform=%t --delimiter %q --query {q} -- {+}`,
		currentCommand,
		varName,
		isRequired,
		allowFreeform,
		delimiter,
	)
	fzfArgs = append(fzfArgs, bindingArg(fmt.Sprintf("ctrl-t:become(echo $(%s) && %s)", preview, preview)))
	fmt.Println(preview)
	if *varDef.Multi {
		fzfArgs = append(fzfArgs, multiArg())
	}

	if isRequired {
		// Required var, don't allow skipping with ESC
		fzfArgs = append(fzfArgs, bindingArg("esc:ignore"))
		if allowFreeform {
			fzfArgs = append(fzfArgs, bindingArg(fmt.Sprintf("enter:transform:%v && echo accept-or-print-query || echo ignore", valuePresentCheck)))
		} else {
			// Required variable with no freeform allowed
			fzfArgs = append(fzfArgs, bindingArg("enter:accept-non-empty"))
		}
	} else {
		// Optional variable, can be skipped
		keybindings = append(keybindings, "Esc: Skip")
		// Freeform allowed, if nothing is selected, we take query as value
		if allowFreeform {
			fzfArgs = append(fzfArgs, bindingArg("enter:accept-or-print-query"))
		}
	}

	if allowFreeform {
		prompt = fmt.Sprintf("Enter {{%s}}", varName)
	} else {
		prompt = fmt.Sprintf("Select %s", varName)
	}
	if len(varDef.Options) > 0 {
		options = toChan(varDef.Options)
	} else if varDef.OptionsCmd != "" {
		go executeCommand(varDef.OptionsCmd, options)
	} else {
		close(options)
	}

	fzfArgs = append(fzfArgs, promptArg(prompt+": "), previewArg(preview))
	// we can only execute directly if query is filled or entry is selected AND this var is the last one to be replaced
	keybindingsLabel := strings.Join(keybindings, " | ")
	if isLast {
		inputLabelTransform := fmt.Sprintf(`focus:transform-input-label:%v && echo '%v' || echo '%v'`, valuePresentCheck, keybindingsLabel+" | Ctrl-E: Execute", keybindingsLabel)
		exec := fmt.Sprintf("ctrl-e:transform:%v && echo 'become(eval $(%v))' || echo ignore", valuePresentCheck, strings.ReplaceAll(preview, `'`, `\'`))
		fzfArgs = append(fzfArgs, bindingArg(inputLabelTransform), bindingArg(exec))
	} else {
		fzfArgs = append(fzfArgs, inputLabelArg(keybindingsLabel))
	}

	selected, _, err := invokeFzf(options, fzfArgs)

	if err != nil {
		return "", fmt.Errorf("failed to select variable %q: %w", varName, err)
	}
	return strings.Join(selected, delimiter), nil
}

func selectCommand(cmds []Command, commandVariables map[int][]string) (*Command, int, error) {
	input := make(chan string, len(cmds))
	go func() {
		defer close(input)
		for idx, cmd := range cmds {
			inputLabel := []string{"Ctrl-C: Quit"}
			executeAction := "ignore"

			// If command has no vars, direct execution can be triggered
			if len(commandVariables[idx]) == 0 {
				inputLabel = append(inputLabel, "Ctrl-E: Execute")
				executeAction = "become(eval $(echo {3}))"
			}

			// Cmd Index | Cmd Title | Cmd | Keybinds | Execute Action
			input <- formatLine(strconv.Itoa(idx), cmd.Title, cmd.Cmd,
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
		return nil, -1, fmt.Errorf("command selection failed: %w", err)
	}

	if len(selection) == 0 || rc == fzf.ExitInterrupt {
		return nil, -1, nil
	}

	commandIndex, err := strconv.Atoi(selection[0])
	if err != nil {
		return nil, -1, err
	}
	if commandIndex >= len(cmds) {
		return nil, -1, fmt.Errorf("command index not found: %v", commandIndex)
	}

	cmd := cmds[commandIndex]
	return &cmd, commandIndex, nil
}

func extractVariableNames(template string) []string {
	variabes := varPatternRegex.FindAllStringSubmatch(template, -1)

	uniqueVars := make([]string, 0, len(variabes))

	seen := make(map[string]bool, len(variabes))
	for _, elem := range variabes {
		if len(elem) > 1 {
			varName := strings.TrimSpace(elem[1])
			if !seen[varName] {
				uniqueVars = append(uniqueVars, varName)
				seen[varName] = true
			}
		}
	}

	return uniqueVars
}

func runSearch(config Config) error {
	var err error

	commandVariables := map[int][]string{}
	for idx, cmd := range config.Commands {
		commandVariables[idx] = extractVariableNames(cmd.Cmd)
	}

	cmd, cmdIdx, err := selectCommand(config.Commands, commandVariables)
	if err != nil || cmd == nil {
		return err
	}

	mergedVars := mergeVars(config.GlobalVars, cmd.Vars)

	fullCmd, err := replaceVariables(cmd.Cmd, commandVariables[cmdIdx], mergedVars)
	if err != nil {
		return err
	}
	fmt.Println(fullCmd)

	return nil
}

func replacePlaceholder(template, varName, value string) string {
	return regexp.MustCompile(fmt.Sprintf(`{{\s*%s\s*}}`, varName)).ReplaceAllString(template, value)
}
