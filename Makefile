.PHONY: check fmt fmt-check race test vet

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

check: fmt-check vet test
