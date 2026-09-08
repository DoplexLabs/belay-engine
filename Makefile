PREVIEW_VERSION ?= 0.0.1-dev.1
PREVIEW_ARCH ?= native
PREVIEW_OUTPUT ?= ./dist

.PHONY: test verify preview preview-all smoke-preview

test:
	CGO_ENABLED=0 go test -count=1 ./...

verify: test
	go vet ./...
	bash -n scripts/build-developer-preview.sh
	bash -n scripts/smoke-developer-preview.sh

preview:
	scripts/build-developer-preview.sh \
		--version "$(PREVIEW_VERSION)" \
		--arch "$(PREVIEW_ARCH)" \
		--output-dir "$(PREVIEW_OUTPUT)"

preview-all:
	scripts/build-developer-preview.sh \
		--version "$(PREVIEW_VERSION)" \
		--arch all \
		--output-dir "$(PREVIEW_OUTPUT)"

smoke-preview:
	@test -n "$(ARCHIVE)" || (echo "usage: make smoke-preview ARCHIVE=path/to/archive.tar.gz" >&2; exit 1)
	scripts/smoke-developer-preview.sh "$(ARCHIVE)"
