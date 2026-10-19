APP_NAME ?= snippetbook
DIST_DIR ?= dist
BIN_DIR ?= bin

GO ?= go
GOFLAGS ?=
LDFLAGS ?= -s -w
VERSION ?= dev
COMMIT ?= unknown
BUILD_TIME ?= unknown
BUILD_LDFLAGS = $(LDFLAGS) -X snippetbook/internal/version.Version=$(VERSION) -X snippetbook/internal/version.Commit=$(COMMIT) -X snippetbook/internal/version.BuildTime=$(BUILD_TIME)

PLATFORMS := \
	linux-amd64 \
	linux-arm64 \
	darwin-amd64 \
	darwin-arm64 \
	windows-amd64 \
	windows-arm64 \
	freebsd-amd64 \
	freebsd-arm64

.DEFAULT_GOAL := help

.PHONY: help fmt test vet build run check release release-local clean tidy

help:
	@printf '%s\n' \
		'Доступные команды:' \
		'  make build                 локальная сборка в bin/' \
		'  make release               сборка для всех поддерживаемых платформ' \
		'  make release-local         сборка для текущей системы и процессора' \
		'  make run                   запуск с загрузкой .env из текущего каталога' \
		'  make release-linux-amd64   сборка для конкретной платформы' \
		'  make check                 fmt, test, vet и build' \
		'  make tidy                  обновить go.mod и go.sum' \
		'  make clean                 удалить bin/ и dist/'

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

build:
	@mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags "$(BUILD_LDFLAGS)" -o "$(BIN_DIR)/$(APP_NAME)" .

run:
	$(GO) run .

check: fmt test vet build

tidy:
	$(GO) mod tidy

release: $(PLATFORMS:%=release-%)
	@printf 'Готовые сборки находятся в %s/\n' "$(DIST_DIR)"

release-local:
	@platform="$$($(GO) env GOOS)-$$($(GO) env GOARCH)"; \
	printf 'Сборка для текущей платформы: %s\n' "$$platform"; \
	$(MAKE) "release-$$platform"

release-%:
	@platform="$*"; \
	case "$$platform" in \
		linux-amd64|linux-arm64|darwin-amd64|darwin-arm64|windows-amd64|windows-arm64|freebsd-amd64|freebsd-arm64) ;; \
		*) echo "Неподдерживаемая платформа: $$platform" >&2; exit 2 ;; \
	esac; \
	os="$${platform%%-*}"; \
	arch="$${platform#*-}"; \
	out="$(DIST_DIR)/$$platform"; \
	case "$$os" in windows) suffix='.exe' ;; *) suffix='' ;; esac; \
	mkdir -p "$$out"; \
	GOOS="$$os" GOARCH="$$arch" CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags "$(BUILD_LDFLAGS)" -o "$$out/$(APP_NAME)$$suffix" .; \
	cp -R web "$$out/web"; \
	cp .env.example "$$out/.env.example"; \
	printf 'Собрано: %s\n' "$$out/$(APP_NAME)$$suffix"

clean:
	rm -rf "$(BIN_DIR)" "$(DIST_DIR)"
