.PHONY: all build test clean

all: build

build:
	go build -trimpath -ldflags="-s -w" -o px1 .

test:
	go test ./...

clean:
	go clean
