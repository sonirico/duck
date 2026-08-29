default:
    just --list

build:
    go build -o duck .

test:
    go test ./...

check:
    go build ./...
    go vet ./...
    go test ./...
    test -z "$(gofmt -l .)"

fmt:
    gofmt -w .

smoke:
    ./scripts/smoke.sh

demo-up:
    ./scripts/demo.sh up

demo-down:
    ./scripts/demo.sh down

record: build demo-up
    for tape in vhs/*.tape; do vhs "$tape"; done
    just demo-down
