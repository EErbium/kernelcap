.PHONY: all build-oss build-enterprise test-all lint clean

GO      := go
BINDIR  := bin
EE_DIR  := ee
OSS_CMD := ./cmd/agent

# Cross-platform command aliases
ifeq ($(OS),Windows_NT)
RMDIR := rmdir /s /q
RM    := del /q
else
RMDIR := rm -rf
RM    := rm -f
endif

# Detect whether the enterprise module is present
EE_EXISTS := $(if $(wildcard $(EE_DIR)/go.mod),1,0)

all: build-oss

# ── Open-Source Build ──────────────────────────────────────
build-oss:
	@echo ">>> Building OSS agent binary..."
	$(GO) build -o $(BINDIR)/profiler-agent $(OSS_CMD)
	@echo ">>> Done: $(BINDIR)/profiler-agent"

# ── Enterprise Build (conditional) ─────────────────────────
build-enterprise:
ifeq ($(EE_EXISTS),1)
	@echo ">>> Building enterprise control-plane..."
	cd $(EE_DIR) && $(GO) build -o ../$(BINDIR)/control-plane ./control-plane/cmd
	@echo ">>> Done: $(BINDIR)/control-plane"
else
	@echo ">>> Skipping enterprise build — no /ee module found"
endif

# ── Test All ───────────────────────────────────────────────
test-all:
	@echo ">>> Running OSS tests..."
	$(GO) test -count=1 -race ./...
ifeq ($(EE_EXISTS),1)
	@echo ">>> Running enterprise tests..."
	cd $(EE_DIR) && $(GO) test -count=1 -race ./...
endif
	@echo ">>> All tests passed"

# ── Lint ───────────────────────────────────────────────────
lint:
	@echo ">>> Running go vet on OSS..."
	$(GO) vet ./...
ifeq ($(EE_EXISTS),1)
	@echo ">>> Running go vet on enterprise..."
	cd $(EE_DIR) && $(GO) vet ./...
endif
	@echo ">>> Lint complete"

# ── Clean ──────────────────────────────────────────────────
clean:
	@echo ">>> Cleaning build artifacts..."
	$(GO) clean -i ./...
	-$(RMDIR) $(BINDIR) 2>NUL
	-$(RM) coverage.txt coverage.html 2>NUL
	-$(RM) __debug_bin 2>NUL
	@echo ">>> Clean complete"
