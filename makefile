PACKAGE=github.com/tcfw/otter
VERSION=$(shell git describe --tags --always --abbrev=0 --match='v[0-9]*.[0-9]*.[0-9]*' 2> /dev/null | sed 's/^.//')
COMMIT_HASH=$(shell git rev-parse --short HEAD)
BUILD_TIMESTAMP=$(shell date '+%Y-%m-%dT%H:%M:%S')

LDFLAGS=-X '${PACKAGE}/internal/version.version=${VERSION}' -X '${PACKAGE}/internal/version.commitHash=${COMMIT_HASH}' -X '${PACKAGE}/internal/version.buildTime=${BUILD_TIMESTAMP}'
PLUGINS=$(shell ls ./pkg/protos/)

PROTOBUFS=$(shell find ./pkg -iname "*.proto") $(shell find ./internal -iname "*.proto")

.PHONY: run
run:
	go run -ldflags="${LDFLAGS}" .

.PHONY: dist
dist:
	go build ${TAGS} -ldflags="-w -s ${LDFLAGS}" -o dist/otter .

clean:
	rm -rf ./dist/*

.PHONY: ${PLUGINS}
${PLUGINS}:
	go build -buildmode=plugin -tags plugin -o dist/plugins/$@.so ./pkg/protos/$@/plugin

.PHONY: plugins
plugins: ${PLUGINS}

.PHONY: protos
protos: $(PROTOBUFS)

.PHONY: $(PROTOBUFS)
$(PROTOBUFS):
	protoc --go_out=. --go_opt=paths=source_relative $@