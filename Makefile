.PHONY: build run test fmt vet docker

BIN := arcticexpress

build:
	go build -o $(BIN) ./cmd/server

run: build
	./$(BIN)

test:
	go test -timeout=120s -count=1 ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

docker:
	docker build -t arcticexpress:latest .
