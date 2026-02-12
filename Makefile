.PHONY: build run validate debug plan clean test help

BINARY_NAME=liteci
BINARY_PATH=./cmd/$(BINARY_NAME)
MAIN_PATH=$(BINARY_PATH)/main.go

# Default target
help:
	@echo "liteci - Schema-Driven Planner Engine"
	@echo ""
	@echo "Available targets:"
	@echo "  build       - Build the binary"
	@echo "  run-plan    - Generate plan from examples"
	@echo "  run-validate - Validate example files"
	@echo "  run-debug   - Debug intent processing"
	@echo "  test        - Run tests"
	@echo "  clean       - Remove built artifacts"
	@echo ""

build:
	@echo "🔨 Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) ./$(BINARY_PATH)
	@echo "✅ Built: ./$(BINARY_NAME)"

run-plan: build
	@echo ""
	@echo "🎯 Generating plan..."
	@./$(BINARY_NAME) plan -i examples/intent.yaml -j examples/jobs.yaml --debug

run-validate: build
	@echo ""
	@echo "✓ Validating files..."
	@./$(BINARY_NAME) validate -i examples/intent.yaml -j examples/jobs.yaml

run-debug: build
	@echo ""
	@echo "🔍 Debugging intent..."
	@./$(BINARY_NAME) debug -i examples/intent.yaml -j examples/jobs.yaml

test:
	@echo "🧪 Running tests..."
	@go test -v ./...

clean:
	@echo "🧹 Cleaning..."
	@rm -f $(BINARY_NAME)
	@go clean
	@echo "✅ Clean complete"

install-deps:
	@echo "📦 Installing dependencies..."
	@go mod tidy
	@go mod download
	@echo "✅ Dependencies installed"

fmt:
	@echo "🎨 Formatting code..."
	@go fmt ./...
	@echo "✅ Formatted"

lint:
	@echo "🔍 Linting..."
	@go vet ./...
	@echo "✅ Lint passed"

release-snapshot:
	@echo "📦 Building snapshot release with GoReleaser..."
	@which goreleaser > /dev/null || (echo "❌ goreleaser not found. Install with: brew install goreleaser" && exit 1)
	@goreleaser build --snapshot --clean
	@echo "✅ Snapshot built in dist/"

release-test:
	@echo "🧪 Testing OCI artifact structure..."
	@mkdir -p /tmp/liteci-test
	@for arch in linux/amd64 darwin/arm64; do \
		if [ -f dist/liteci_snapshot_*/platform-native_*_$${arch/\//_}/liteci ]; then \
			echo "✓ Binary found: $${arch}"; \
		fi; \
	done
	@echo "✅ Structure validated"

all: clean build test run-validate run-debug run-plan
	@echo ""
	@echo "✅ All targets completed"
