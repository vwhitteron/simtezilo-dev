# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'


# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

buildversion := $(shell git describe --tags --always --dirty)
buildtime := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

## build/darwin/silicon: build the application for Apple Silicon
.PHONY: build/darwin/silicon
build/darwin/silicon:
	@GOOS=darwin GOARCH=arm64 docker build \
	--output=out --target=binaries --progress=plain 
	-f build/docker/Dockerfile .

## build/rpi: build the application for Raspberry Pi using ARMHF (any version)
.PHONY: build/rpi
build/rpi:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARCH=arm \
	--build-arg BUILDTIME=$(buildtime) --build-arg BUILTVERSION=$(buildversion) \
	--output=out --target=binaries-armhf --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v6: build the application for Raspberry Pi ARMv6 (1*, Zero*)
.PHONY: build/rpi/v6
build/rpi/v6:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARCH=arm --build-arg GOARM=6 \
	--build-arg BUILDTIME=$(buildtime) --build-arg BUILTVERSION=$(buildversion) \
	--output=out --target=binaries-armel --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v7: build the application for Raspberry Pi ARMv7 (2B)
.PHONY: build/rpi/v7
build/rpi/v7:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARM=7 \
	--build-arg BUILDTIME=$(buildtime) --build-arg BUILTVERSION=$(buildversion) \
	--output=out --target=binaries-armel --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v8/32: build the application for Raspberry Pi ARMv8 32bit (2B[+1.2], 3*, 4*, Zero 2W)
.PHONY: build/rpi/v8/32
build/rpi/v8/32:
	@docker build \
	--build-arg GOOS=linux \
	--build-arg BUILDTIME=$(buildtime) --build-arg BUILTVERSION=$(buildversion) \
	--output=out --target=binaries-armel-8 --progress=plain \
	-f build/docker/Dockerfile .

## build/rpi/v8/64: build the application for Raspberry Pi ARMv8 64bit (3*, 4*, Zero 2W)
.PHONY: build/rpi/v8/64
build/rpi/v8/64:
	@docker build \
	--build-arg GOOS=linux --build-arg GOARCH=arm64 \
	--build-arg BUILDTIME=$(buildtime) --build-arg BUILTVERSION=$(buildversion) \
	--output=out --target=binaries-arm64-8 --progress=plain \
	-f build/docker/Dockerfile .

## run/live: run the application with reloading on file changes
.PHONY: run/live
run/live:
	@go run cmd/haptics/main.go -r -l debug -p=false -w=true

## clean: clean up build output files
.PHONY: clean
clean:
	@rm -rf out
	@go clean -cache

## clean: clean up project and return to a pristine state
.PHONY: distclean
distclean: clean
	@docker builder prune -af