# cmdmark

A small fzf-driven command-bookmark tool: store shell command templates in YAML, pick a template with fzf and fill variables interactively.

Features

- fzf powered search for commands and options
- free-form vars, predefined options or options_cmd
- single or multi-selection with custom delimiters

## Example

```yaml
vars:
  containers:
    multi: true
    delimiter: " "
    options_cmd: docker ps --format '{{.Names}}'

commands:
  - title: Stop Docker Containers
    cmd: docker stop {{containers}}

  - title: Find Files by Extensions
    cmd: find {{path}} -type f -name '*.{{ext}}'
    vars:
      ext:
        multi: false
        options:
          - py
          - go
          - js
```

## Install & Run

### Go

```bash
go install github.com/tarow/cmdmark@latest
cmdmark config.yml
```

### Nix

```bash
nix run github:tarow/cmdmark config.yml

```

## Shell Integration

`cmdmark` will print the selected command.
To put the selected command into the command line buffer, you can integrate cmdmark with your shell:

### Bash

```sh
function cmdmark-select() {
  BUFFER=$(cmdmark ~/.config/cmdmark/config.yml)
  READLINE_LINE=$BUFFER
  READLINE_POINT=${#BUFFER}
}
bind -x '"\C-b": cmdmark-select'
```

### Zsh

```sh
function cmdmark-select() {
  BUFFER=$(cmdmark ~/.config/cmdmark/config.yml)
  CURSOR=$#BUFFER
  zle redisplay
}
zle -N cmdmark-select
bindkey '^b' cmdmark-select
```

### Fish

```sh
function cmdmark-select
    commandline -r -- $(cmdmark ~/.config/cmdmark/config.yml)
end
bind \cb cmdmark-select
```
