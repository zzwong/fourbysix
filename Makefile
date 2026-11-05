# fourbysix — build and checks.

BIN := bin/fourbysix

.PHONY: build check fmt vet test static

# build produces the single static binary.
build:
	CGO_ENABLED=0 go build -trimpath -o $(BIN) ./cmd/fourbysix

# check is the full gate: formatting, vet, tests, a static build, and a
# static-linkage check.
check: fmt vet test build static

fmt:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then echo "gofmt needed:"; echo "$$files"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./...

# static verifies the binary really is statically linked. ldd exits non-zero
# for a static binary, hence the explicit message match. Only Linux has a
# fully static Go binary: on macOS Go always links libSystem, so the check
# is skipped there.
static:
	@if [ "$$(uname -s)" != Linux ]; then echo "$(BIN): static check skipped (Linux only)"; exit 0; fi; \
	out="$$(ldd $(BIN) 2>&1 || true)"; \
	case "$$out" in \
		*"not a dynamic executable"*|*"statically linked"*) echo "$(BIN): static" ;; \
		*) echo "$(BIN): not static:"; echo "$$out"; exit 1 ;; \
	esac
