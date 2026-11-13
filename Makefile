# install-remap and install-logrotate need root.
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/go-toolchain/go/bin/go)
GOFMT ?= $(dir $(GO))gofmt
HZ ?= $(shell command -v hz 2>/dev/null || echo $(HOME)/go-toolchain/go/bin/hz)
THRIFT2OPENAPI ?= $(shell command -v thrift2openapi 2>/dev/null || echo $(HOME)/go-toolchain/go/bin/thrift2openapi)
TAGS ?= stdjson
VERSION ?= dev
ROOT := $(CURDIR)
RUN_USER ?= $(if $(SUDO_USER),$(SUDO_USER),$(shell id -un))
# The agent's own log lives in the user's state dir, never in the checkout: a
# log line is one careless "text=..." away from holding transcripts, and a file
# inside the repository gets read, attached and committed (SEC-02). install-logrotate
# runs under sudo, where $(HOME) is root's, so the home is looked up for the
# service user instead.
RUN_HOME := $(shell getent passwd $(RUN_USER) 2>/dev/null | cut -d: -f6)
ifeq ($(RUN_HOME),)
RUN_HOME := $(HOME)
endif
RUN_UID := $(shell id -u $(RUN_USER) 2>/dev/null)
RUN_GID := $(shell id -g $(RUN_USER) 2>/dev/null)
LOG_DIR := $(RUN_HOME)/.local/state/xiaomi-remote
LOG_FILE := $(LOG_DIR)/agent.log
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
	$(GO) run ./script/thriftctx biz/model/api/xiaomi_remote.go

openapi:
	$(THRIFT2OPENAPI) -idl idl/xiaomi_remote.thrift -o docs/openapi.yaml -title "Open Voice Bridge API" -version 0.1.0 -description "Local RC003 and external input bridge API"

soak:
	DURATION=$${DURATION:-300} script/soak.sh

install-user: build
	mkdir -p $(dir $(USER_UNIT))
	sed -e 's|@ROOT@|$(ROOT)|g' -e 's|@USER@|$(RUN_USER)|g' \
		-e 's|@LOG_DIR@|$(LOG_DIR)|g' -e 's|@LOG_FILE@|$(LOG_FILE)|g' \
		deploy/xiaomi-remote-go.service > $(USER_UNIT)
	chmod 644 $(USER_UNIT)
	systemctl --user daemon-reload
	systemctl --user enable --now xiaomi-remote-go.service

install-remap:
	mkdir -p $(dir $(SYSTEM_UNIT))
	sed -e 's|@ROOT@|$(ROOT)|g' -e 's|@USER@|$(RUN_USER)|g' \
		-e 's|@UID@|$(RUN_UID)|g' -e 's|@GID@|$(RUN_GID)|g' \
		deploy/xiaomi-remote-remap.service > $(SYSTEM_UNIT)
	chmod 644 $(SYSTEM_UNIT)
	systemctl daemon-reload
	systemctl enable --now xiaomi-remote-remap.service

install-logrotate:
	sed -e 's|@ROOT@|$(ROOT)|g' -e 's|@USER@|$(RUN_USER)|g' \
		-e 's|@LOG_FILE@|$(LOG_FILE)|g' \
		deploy/logrotate-xiaomi-remote-go > /etc/logrotate.d/xiaomi-remote-go
	chmod 644 /etc/logrotate.d/xiaomi-remote-go