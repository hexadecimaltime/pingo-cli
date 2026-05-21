set windows-shell := ["powershell.exe", "-NoProfile", "-c"]

@default:
    just --list

# Install dependencies
setup:
    uv sync

# Build Go binary
@build:
    go build -o pingo.exe .

# Run application in development (Go)
@run *args:
    go run . {{args}}

# Run legacy Python application
@run-py *args:
    uv run python/pingo.py {{args}}

# Build standalone Windows executable using Nuitka
@build-py:
    uv run nuitka --onefile --standalone --remove-output \
    --windows-product-name="PingoCLI" \
    --windows-company-name="LocalDev (0xtime)" \
    --windows-file-version=0.1.0 \
    --output-filename=pingo python/pingo.py