.PHONY: build run dev tidy clean test generate

BINARY=bin/server
TEMPL=$(HOME)/go/bin/templ

generate:
	$(TEMPL) generate

build: generate
	go build -o $(BINARY) ./cmd/server/

run: build
	./$(BINARY)

dev: generate
	go run ./cmd/server/

tidy:
	go mod tidy

clean:
	rm -rf bin/ tmp/

test:
	go test ./... -v
