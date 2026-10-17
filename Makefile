VERSION ?= 0.1.0
BINARY  = uucpmail-client

.PHONY: build test vet run clean cross

build:
	go build -o bin/$(BINARY) ./cmd/uucpmail-client

test:
	go test ./...

vet:
	go vet ./...

run: build
	./bin/$(BINARY)

clean:
	rm -rf bin dist

# Cross-Builds für Linux/Windows via fyne-cross (benötigt Docker):
#   go install github.com/fyne-io/fyne-cross@latest
# macOS kann nicht per Docker cross-gebaut werden – dafür die CI-Matrix nutzen.
cross:
	fyne-cross linux -arch amd64 -app-id de.uucpmail.client ./cmd/uucpmail-client
	fyne-cross windows -arch amd64 -app-id de.uucpmail.client ./cmd/uucpmail-client
