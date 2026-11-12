# See README.md. Everything runs from the pinned inputs in internal/gen/toolchain.env.
.PHONY: all generate verify-generate dwebp testdata test race lint fuzz bench bench-docker clean

all: test

## generate: rebuild internal/gen/libwebp.go from the pinned libwebp release and toolchain
generate:
	./scripts/generate.sh

## verify-generate: fail if regenerating internal/gen/libwebp.go changes a single byte
verify-generate:
	./scripts/verify-generate.sh

## dwebp: build libwebp's own dwebp and cwebp natively (the reference for tests and benchmarks)
dwebp:
	./scripts/build-dwebp.sh >/dev/null

## testdata: regenerate the fixtures in testdata/ with libwebp's cwebp and dwebp
testdata: dwebp
	go run ./internal/testimg/mktestdata -bin .cache/bin -o testdata

test: dwebp
	go test ./...

race: dwebp
	go test -race ./...

lint:
	go vet ./...
	golangci-lint run ./...

## fuzz: a short fuzzing run of Decode (FUZZTIME=30s by default)
fuzz:
	go test -run '^$$' -fuzz FuzzDecode -fuzztime $${FUZZTIME:-30s} .

## bench: the benchmark against native libwebp and other Go WebP packages; prints a Markdown table
bench:
	./bench/run.sh

## bench-docker: the same in a Linux container limited to 1 CPU (PLATFORM=linux/arm64 by default)
bench-docker:
	./bench/docker.sh $${PLATFORM:-linux/arm64} $${CPUS:-1}

clean:
	rm -rf .cache third_party/libwebp/_src bench/work
