.PHONY: build run test fmt lint clean

build:
	@./scripts/build-daz.sh

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