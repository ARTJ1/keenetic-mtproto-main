.PHONY: ui build test release-local clean

UI_DIR=ui
DIST=internal/web/dist
BIN=bin/keenetic-mtproto

ui:
	cd $(UI_DIR) && npm install && npm run build

test:
	go test ./internal/...

build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/keenetic-mtproto

# Cross-compile the Keenetic targets locally (no UI rebuild).
release-local:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/keenetic-mtproto-linux-amd64 ./cmd/keenetic-mtproto
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/keenetic-mtproto-linux-arm64 ./cmd/keenetic-mtproto
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" -o dist/keenetic-mtproto-linux-armv7 ./cmd/keenetic-mtproto
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -ldflags="-s -w" -o dist/keenetic-mtproto-linux-mipsle_softfloat ./cmd/keenetic-mtproto

clean:
	rm -rf bin dist
