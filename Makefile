.PHONY: lint lintfix test jstypes mjstypes test-report

lint:
	golangci-lint run -c ./golangci.yml ./...

lintfix:
	golangci-lint run -c ./golangci.yml ./... --fix

test:
	go test ./... -v --cover

jstypes:
	go run ./plugins/jsvm/internal/types/types.go

mjstypes:
	go run ./plugins/esmvm/internal/types/types.go

test-report:
	go test ./... -v --cover -coverprofile=coverage.out
	go tool cover -html=coverage.out
