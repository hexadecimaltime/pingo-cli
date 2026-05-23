set windows-shell := ["powershell.exe", "-NoProfile", "-c"]

@default:
    just --list

@build:
    go mod tidy
    go run github.com/tc-hib/go-winres@latest make --in .winres/winres.json --out .
    go build -o pingo.exe .

# Run application in development
@run *args:
    go run . {{args}}
