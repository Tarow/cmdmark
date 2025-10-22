package main

import (
	"fmt"
	"maps"
	"os"

	"go.yaml.in/yaml/v4"
)

type Config struct {
	GlobalVars      map[string]VarDefinition `yaml:"vars"`
	GlobalFragments map[string]VarDefinition `yaml:"fragments"`
	Commands        []Command                `yaml:"commands"`
}

type VarDefinition struct {
	Options    []string `yaml:"options"`
	OptionsCmd string   `yaml:"options_cmd"`
	Multi      bool     `yaml:"multi"`
	Delimiter  string   `yaml:"delimiter"`
}

type Command struct {
	Title string                   `yaml:"title"`
	Cmd   string                   `yaml:"cmd"`
	Vars  map[string]VarDefinition `yaml:"vars"`
}

func loadConfig(filename string) (Config, error) {
	var config Config

	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("failed to load config: %w", err)
	}

	return config, nil
}

func mergeVars(globalVars map[string]VarDefinition, commandVars map[string]VarDefinition) map[string]VarDefinition {
	result := globalVars
	if result == nil {
		result = make(map[string]VarDefinition)
	}
	maps.Copy(result, commandVars)
	return result
}
