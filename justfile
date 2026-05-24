set windows-shell := ["powershell.exe", "-NoProfile", "-c"]

# Global Variables
app_name := "pingo"
bin_dir := "bin"
release_flags := "-trimpath -ldflags=\"-s -w\""

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
    go run github.com/tc-hib/go-winres@latest make --in .winres/winres.json --out .
    New-Item -ItemType Directory -Force -Path {{bin_dir}} | Out-Null
    $env:GOOS='windows'; $env:GOARCH='amd64'; go build -o {{bin_dir}}/{{app_name}}.exe .

# Build for Unix-like systems (macOS, Linux)
[unix]
[group("build")]
@build: clean
    go mod tidy
    mkdir -p {{bin_dir}}
    go build -o {{bin_dir}}/{{app_name}} .

# Cross-compile for a specific OS/ARCH (Host: Windows)
[windows]
[group("build")]
@cross target_os target_arch:
    {{ if target_os == "windows" { "go run github.com/tc-hib/go-winres@latest make --in .winres/winres.json --out ." } else { "" } }}
    New-Item -ItemType Directory -Force -Path {{bin_dir}} | Out-Null
    $env:CGO_ENABLED='0'; $env:GOOS='{{target_os}}'; $env:GOARCH='{{target_arch}}'; go build {{release_flags}} -o {{bin_dir}}/{{app_name}}-{{target_os}}-{{target_arch}}{{ if target_os == "windows" { ".exe" } else { "" } }} .

# Cross-compile for a specific OS/ARCH (Host: Unix)
[unix]
[group("build")]
@cross target_os target_arch:
    {{ if target_os == "windows" { "go run github.com/tc-hib/go-winres@latest make --in .winres/winres.json --out ." } else { "" } }}
    mkdir -p {{bin_dir}}
    CGO_ENABLED=0 GOOS={{target_os}} GOARCH={{target_arch}} go build {{release_flags}} -o {{bin_dir}}/{{app_name}}-{{target_os}}-{{target_arch}}{{ if target_os == "windows" { ".exe" } else { "" } }} .

# Run application in development
[group("dev")]
@run *args:
    go run . {{args}}

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