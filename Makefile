BINARY    := temporal-utility
IMAGE_TAG ?= temporal-utility:latest

.PHONY: build run test lint tidy clean docker swag lambda zip

build:
	go build -o $(BINARY) .

run:
	go run .

test:
	go test ./... -race -count=1

lint:
	golangci-lint run

tidy:
	go mod tidy

swag:
	swag init --generalInfo main.go --output docs

docker:
	docker build -t $(IMAGE_TAG) .

clean:
	rm -f $(BINARY)

lambda:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bootstrap .

zip: lambda
	zip -j bootstrap.zip bootstrap
