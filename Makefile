# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'


# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## audit: run quality control checks
.PHONY: audit
audit: test
	go mod tidy -diff
	go mod verify
	test -z "$(shell gofmt -l .)" 
	go vet ./...
#   waiting for fix: https://github.com/dominikh/go-tools/issues/1653
# 	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## test: run all tests
.PHONY: test
test:
	go test -v -race -buildvcs ./...

## test/watch: run all tests re-run when any files change
.PHONY: test/watch
test/watch:
	go run github.com/mitranim/gow@latest \
	-c \
	-e=go,mod,html,js,svg,png \
	test -v -race -buildvcs ./...

## test/cover: run all tests and display coverage
.PHONY: test/cover
test/cover:
	go test -v -race -buildvcs -coverprofile=out/coverage.out ./...
	go tool cover -html=out/coverage.out

## upgradeable: list direct dependencies that have upgrades available
.PHONY: upgradeable
upgradeable:
	@go run github.com/oligot/go-mod-upgrade@latest


# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

buildmodule := $(shell awk '/module/ {print $$NF}' go.mod)
buildversion := $(shell git describe --tags --always --dirty)
buildtime := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

## lint: run linter against project
.PHONY: lint
lint:
	@golangci-lint run

## lint/fix: run linter against the project and fix issues where possible
.PHONY: lint/fix
lint/fix:
	@golangci-lint run --fix

## build: build the application for the current platform
.PHONY: build
build:
	@go build -ldflags "-X '$(buildmodule)/app.Version=$(buildversion)' -X '$(buildmodule)/app.BuildTime=$(buildtime)'" \
	-o ./out/simtezilo-local ./cmd/simtezilo/main.go

## build/darwin/silicon: build the application for Apple Silicon
.PHONY: build/darwin/silicon
build/darwin/silicon:
	GOOS=darwin GOARCH=arm64 \
	go build -ldflags "-X '$(buildmodule)/app.Version=$(buildversion)' -X '$(buildmodule)/app.BuildTime=$(buildtime)'" \
	-o ./out/simtezilo-macos ./cmd/simtezilo/main.go

## build/darwin/silicon: build the application for Apple Silicon
.PHONY: build/windows/64
build/windows/64:
	GOOS=windows GOARCH=amd64 \
	go build -ldflags "-X '$(buildmodule)/app.Version=$(buildversion)' -X '$(buildmodule)/app.BuildTime=$(buildtime)'" \
	-o ./out/simtezilo.exe ./cmd/simtezilo/main.go

## build/rpi: build the application for Raspberry Pi using ARMHF (any version)
.PHONY: build/rpi
build/rpi:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARCH=arm \
	--build-arg BUILDMODULE=$(buildmodule) \
	--build-arg BUILDTIME=$(buildtime) \
	--build-arg BUILDVERSION=$(buildversion) \
	--output=out --target=binaries-armhf --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v6: build the application for Raspberry Pi ARMv6 (1*, Zero*)
.PHONY: build/rpi/v6
build/rpi/v6:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARCH=arm --build-arg GOARM=6 \
	--build-arg BUILDMODULE=$(buildmodule) \
	--build-arg BUILDTIME=$(buildtime) \
	--build-arg BUILDVERSION=$(buildversion) \
	--output=out --target=binaries-armel --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v7: build the application for Raspberry Pi ARMv7 (2B)
.PHONY: build/rpi/v7
build/rpi/v7:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARM=7 \
	--build-arg BUILDMODULE=$(buildmodule) \
	--build-arg BUILDTIME=$(buildtime) \
	--build-arg BUILDVERSION=$(buildversion) \
	--output=out --target=binaries-armel --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v8/32: build the application for Raspberry Pi ARMv8 32bit (2B[+1.2], 3*, 4*, 5*, Zero 2W)
.PHONY: build/rpi/v8/32
build/rpi/v8/32:
	@docker build \
	--build-arg GOOS=linux \
	--build-arg BUILDMODULE=$(buildmodule) \
	--build-arg BUILDTIME=$(buildtime) \
	--build-arg BUILDVERSION=$(buildversion) \
	--output=out --target=binaries-armel-8 --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v8/64: build the application for Raspberry Pi ARMv8 64bit (3*, 4*, 5*, Zero 2W)
.PHONY: build/rpi/v8/64
build/rpi/v8/64:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARCH=arm64 \
	--build-arg BUILDMODULE=$(buildmodule) \
	--build-arg BUILDTIME=$(buildtime) \
	--build-arg BUILDVERSION=$(buildversion) \
	--output=out --target=binaries-arm64-8 --progress=plain \
	-f build/docker/Dockerfile .

## run: run the application locally
.PHONY: run
run:
	@go run cmd/simtezilo/main.go -l info -w=true

## run/watch: run the application locally and reload on file changes
.PHONY: run/watch
run/watch:
	go run github.com/air-verse/air@latest \
		--build.cmd "make build" --build.bin "out/simtezilo-local" --build.args_bin "-w" --build.delay "100" \
		--build.include_dir "app, cmd" \
		--build.include_ext "go, html, js, png, svg" \
		--build.include_file "simtezilo.conf" \
		--build.send_interrupt "true" \
		--misc.clean_on_exit "true"

## run/profile: run the application with profiling enabled
.PHONY: run/profile
run/profile:
	@go run cmd/simtezilo/main.go -l debug -p=http://localhost:4040 -w=true

## start-pyroscope: start the Pyroscope profiler Docker container
.PHONY: start-pyroscope
start-pyroscope:
	@docker run \
	--name pyroscope \
	--rm --detach \
	-p 4040:4040 \
	-v $(shell pwd)/data/persist/pyroscope:/data \
	grafana/pyroscope:latest
	@echo "Pyroscope started. Access it at http://localhost:4040"

## start-pyroscope: start the Pyroscope profiler Docker container
.PHONY: start-pyroscope
stop-pyroscope:
	@docker stop pyroscope

## clean: clean up build output files
.PHONY: clean
clean:
	@rm -rf out
	@go clean -cache

## dist: create a distribution archive
.PHONY: dist
dist: build/rpi/v6 build/rpi/v8/64 build/windows/64 build/darwin/silicon
	@./build/scripts/gen_dist.sh

## clean: clean up project and return to a pristine state
.PHONY: distclean
distclean: clean
	@rm -rf dist
	@rm -rf data/persist/pyroscope
	@docker builder prune -af