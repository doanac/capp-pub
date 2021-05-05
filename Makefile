linter:=$(shell which golangci-lint 2>/dev/null || echo $(HOME)/go/bin/golangci-lint)

.PHONY: build
build:
	@mkdir -p bin/
	go build -tags seccomp -o bin/capp-pub main.go

.PHONY: test
test:
	go test ./... -v

.PHONY: fmt
fmt:
	@goimports -e -w ./

.PHONY: check
check:
	@test -z $(shell gofmt -l ./ | tee /dev/stderr) || echo "[WARN] Fix formatting issues with 'make fmt'"
	@test -x $(linter) || (echo "Please install linter from https://github.com/golangci/golangci-lint/releases/tag/v1.25.1 to $(HOME)/go/bin")
	$(linter) run
