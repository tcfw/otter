PACKAGE=github.com/tcfw/otter
VERSION=$(shell git describe --tags --always --abbrev=0 --match='v[0-9]*.[0-9]*.[0-9]*' 2> /dev/null | sed 's/^.//')
COMMIT_HASH=$(shell git rev-parse --short HEAD)
BUILD_TIMESTAMP=$(shell date '+%Y-%m-%dT%H:%M:%S')

LDFLAGS=-X '${PACKAGE}/internal/version.version=${VERSION}' -X '${PACKAGE}/internal/version.commitHash=${COMMIT_HASH}' -X '${PACKAGE}/internal/version.buildTime=${BUILD_TIMESTAMP}'
PLUGINS=$(shell ls ./pkg/protos/)

PROTOBUFS=$(shell find ./pkg -iname "*.proto") $(shell find ./internal -iname "*.proto")

JSPROTOBUFS=js_./pkg/otter/pb/remote.proto

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
protos: $(PROTOBUFS) jsprotos

.PHONY: jsprotos
jsprotos: $(JSPROTOBUFS)

.PHONY: $(PROTOBUFS)
$(PROTOBUFS):
	protoc --go_out=. --go_opt=paths=source_relative $@

.PHONY: $(JSPROTOBUFS)
$(JSPROTOBUFS):
	protoc --js_out=import_style=commonjs,binary:./ui/web/src/components/protos $(patsubst js_%,%,$@)
	sed -i "s/var jspb = require('google-protobuf');/import jspb from 'google-protobuf';/g;s/goog.object.extend(exports, proto.remote);/let protos = global.proto; export { goog as default, protos };/g" $(patsubst %.proto,./ui/web/src/components/protos/%_pb.js, $(patsubst js_./%,%,$@))
	# @echo "export {goog as default, protoExport};" >> $(patsubst %.proto,./ui/web/src/components/protos/%_pb.js, $(patsubst js_./%,%,$@))