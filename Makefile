BINARY_NAME := cmdmark

all: clean install tidy build

run: 
	go run . config.yml

build:
	go build -o bin/$(BINARY_NAME) .

clean:
	rm -f bin/*

install:
	go mod download

lint:
	@golangci-lint --version
	golangci-lint run

tidy:
	go mod tidy
