set windows-shell := ["powershell.exe", "-NoProfile", "-c"]

@default:
    just --list

# Install dependencies
setup:
    uv sync

# Run application in development
@run *args:
    uv run main.py {{args}}

# Build standalone Windows executable using Nuitka
@build:
    uv run nuitka --onefile --standalone --remove-output \
    --windows-product-name="PingoCLI" \
    --windows-company-name="LocalDev (0xtime)" \
    --windows-file-version=0.1.0 \
    --output-filename=pingo main.py