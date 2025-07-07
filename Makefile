.PHONY: build run test fmt lint clean

build:
	go build -o bin/daz cmd/daz/main.go

run: build
	./bin/daz

test:
	go test -race ./...

fmt:
	go fmt ./...

lint:
	go vet ./...

clean:
	rm -rf bin/

.DEFAULT_GOAL := build