# install-remap and install-logrotate need root.
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/go-toolchain/go/bin/go)
GOFMT ?= $(dir $(GO))gofmt
HZ ?= $(shell command -v hz 2>/dev/null || echo $(HOME)/go-toolchain/go/bin/hz)
THRIFT2OPENAPI ?= $(shell command -v thrift2openapi 2>/dev/null || echo $(HOME)/go-toolchain/go/bin/thrift2openapi)
TAGS ?= stdjson
VERSION ?= dev
ROOT := $(CURDIR)
RUN_USER ?= $(if $(SUDO_USER),$(SUDO_USER),$(shell id -un))
BIN := bin/xiaomi-remote
USER_UNIT := $(HOME)/.config/systemd/user/xiaomi-remote-go.service
SYSTEM_UNIT := /etc/systemd/system/xiaomi-remote-remap.service

.PHONY: build test vet fmt check idl openapi soak install-user install-remap install-logrotate

build:
	$(GO) build -tags $(TAGS) -ldflags "-X main.version=$(VERSION)" -o $(BIN) .

test:
	$(GO) test -tags $(TAGS) ./...

vet:
	$(GO) vet -tags $(TAGS) ./...

fmt:
	$(GOFMT) -w main.go router.go router_gen.go biz internal

check: fmt test vet

idl:
	PATH="$(dir $(GO)):$$PATH" $(HZ) update -idl idl/xiaomi_remote.thrift

openapi:
	$(THRIFT2OPENAPI) -idl idl/xiaomi_remote.thrift -o docs/openapi.yaml -title "Open Voice Bridge API" -version 0.1.0 -description "Local RC003 and external input bridge API"

soak:
	DURATION=$${DURATION:-300} script/soak.sh

install-user:
	mkdir -p $(dir $(USER_UNIT))
	sed -e 's|@ROOT@|$(ROOT)|g' -e 's|@USER@|$(RUN_USER)|g' deploy/xiaomi-remote-go.service > $(USER_UNIT)
	chmod 644 $(USER_UNIT)
	systemctl --user daemon-reload
	systemctl --user enable --now xiaomi-remote-go.service

install-remap:
	mkdir -p $(dir $(SYSTEM_UNIT))
	sed -e 's|@ROOT@|$(ROOT)|g' -e 's|@USER@|$(RUN_USER)|g' deploy/xiaomi-remote-remap.service > $(SYSTEM_UNIT)
	chmod 644 $(SYSTEM_UNIT)
	systemctl daemon-reload
	systemctl enable --now xiaomi-remote-remap.service

install-logrotate:
	sed -e 's|@ROOT@|$(ROOT)|g' -e 's|@USER@|$(RUN_USER)|g' deploy/logrotate-xiaomi-remote-go > /etc/logrotate.d/xiaomi-remote-go
	chmod 644 /etc/logrotate.d/xiaomi-remote-go