.PHONY: test vet check gui

test:
	go test ./...

vet:
	go vet ./...

check: test vet

gui:
	CGO_ENABLED=1 wails build -tags "wails malgo"
