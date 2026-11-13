APP=maintenance-reminder
MAIN=./cmd/maintenance-reminder

.PHONY: test vet build build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64

test:
	go test ./...

vet:
	go vet ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(APP) $(MAIN)

build-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o $(APP)-linux-amd64 $(MAIN)

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o $(APP)-linux-arm64 $(MAIN)

build-darwin-amd64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o $(APP)-darwin-amd64 $(MAIN)

build-darwin-arm64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o $(APP)-darwin-arm64 $(MAIN)
