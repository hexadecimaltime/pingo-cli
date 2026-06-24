set windows-shell := ["powershell.exe", "-NoProfile", "-c"]

# Global Variables
export VERSION := "0.1.1"
app_name := "pingo"
bin_dir := "bin"
version := VERSION
winres_version := "{{version}}.0"
release_flags := "-trimpath -ldflags=\"-s -w -X main.version={{version}}\""

@default:
    just --list

# Clean the output directory and build artifacts (Windows)
[windows]
[group("build")]
@clean:
    if (Test-Path {{bin_dir}}) { Remove-Item -Recurse -Force {{bin_dir}} }
    if (Test-Path *.syso) { Remove-Item -Force *.syso }

# Clean the output directory and build artifacts (Unix)
[unix]
[group("build")]
@clean:
    rm -rf {{bin_dir}}
    rm -f *.syso

# Build for Windows (with resource embedding)
[windows]
[group("build")]
@build: clean
    go mod tidy
    just update-winres
    go run github.com/tc-hib/go-winres@latest make --in .winres/winres.json --out .
    New-Item -ItemType Directory -Force -Path {{bin_dir}} | Out-Null
    $env:GOOS='windows'; $env:GOARCH='amd64'; go build -ldflags "-X main.version={{version}}" -o {{bin_dir}}/{{app_name}}.exe .

# Build for Unix-like systems (macOS, Linux)
[unix]
[group("build")]
@build: clean
    go mod tidy
    just update-winres
    mkdir -p {{bin_dir}}
    go build -ldflags "-X main.version={{version}}" -o {{bin_dir}}/{{app_name}} .

# Cross-compile for a specific OS/ARCH (Host: Windows)
[windows]
[group("build")]
@cross target_os target_arch:
    just update-winres
    {{ if target_os == "windows" { "go run github.com/tc-hib/go-winres@latest make --in .winres/winres.json --out ." } else { "" } }}
    New-Item -ItemType Directory -Force -Path {{bin_dir}} | Out-Null
    $env:CGO_ENABLED='0'; $env:GOOS='{{target_os}}'; $env:GOARCH='{{target_arch}}'; go build {{release_flags}} -o {{bin_dir}}/{{app_name}}-{{target_os}}-{{target_arch}}{{ if target_os == "windows" { ".exe" } else { "" } }} .

# Cross-compile for a specific OS/ARCH (Host: Unix)
[unix]
[group("build")]
@cross target_os target_arch:
    just update-winres
    {{ if target_os == "windows" { "go run github.com/tc-hib/go-winres@latest make --in .winres/winres.json --out ." } else { "" } }}
    mkdir -p {{bin_dir}}
    CGO_ENABLED=0 GOOS={{target_os}} GOARCH={{target_arch}} go build {{release_flags}} -o {{bin_dir}}/{{app_name}}-{{target_os}}-{{target_arch}}{{ if target_os == "windows" { ".exe" } else { "" } }} .

# Update winres version fields to match {{version}}
[windows]
@update-winres:
    $winresVersion = "{{winres_version}}"; $json = Get-Content .winres/winres.json -Raw | ConvertFrom-Json; $json.RT_VERSION.'#1'.'0000'.fixed.file_version = $winresVersion; $json.RT_VERSION.'#1'.'0000'.fixed.product_version = $winresVersion; $json | ConvertTo-Json -Depth 10 | Set-Content .winres/winres.json

[unix]
@update-winres:
    winres_version="{{winres_version}}"; sed -i.bak -E "s/\"file_version\": \"[^\"]+\"/\"file_version\": \"${winres_version}\"/; s/\"product_version\": \"[^\"]+\"/\"product_version\": \"${winres_version}\"/" .winres/winres.json; rm -f .winres/winres.json.bak

# Run application in development
[group("dev")]
@run *args:
    go run -ldflags "-X main.version={{version}}" . {{args}}

# Format and tidy dependencies (Run before committing)
[group("dev")]
@tidy:
    go mod tidy
    go fmt ./...

# Produce common release binaries for all platforms
[group("release")]
@release: clean
    just cross linux amd64
    just cross linux arm64
    just cross darwin amd64
    just cross darwin arm64
    just cross windows amd64