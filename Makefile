IMAGE ?= timmersuk/logthing
TAG ?= latest
DATA_DIR ?= $(CURDIR)/logthing-data
DOCKER ?= docker
DOCKER_BUILDX ?= docker buildx
VERSION ?= $(TAG)

# Architectures we want binaries for.
ARCHES := amd64 arm64
OSES := linux windows

# Build targets that run inside Docker containers – no pnpm or Go on host.
.PHONY: frontend frontend-docker build-go-docker build-go-local test build syslogsend \
        docker-build docker-run docker-login docker-push docker-buildx-push \
        compose-up compose-down check-release-clean check-release-main \
        release-tag release-patch build-all-arch

# ------------------------------------------------------------------
# 1️⃣ Build the frontend using a node container (pnpm is available there).
# ------------------------------------------------------------------
frontend-docker:
	@echo "Building frontend with node:20-bookworm-slim…"
	@docker run --rm -e CI=true \
	    -v "$(CURDIR):/src" \
	    -w /src/frontend \
	    node:20-bookworm-slim \
	    sh -c "make frontend"

# ------------------------------------------------------------------
# 2️⃣ Build the Go binaries using a golang container.
# ------------------------------------------------------------------
build-go-docker:
	@echo "Building Go binaries for all supported architectures in Docker…"
	docker run --rm \
	            -v "$(CURDIR):/src" \
	            -w /src \
	            golang:1.26-bookworm \
	            sh -c 'make build-go-local'; 

# ------------------------------------------------------------------
# 3️⃣ Public build targets that runs the frontend and Go builds in Docker containers, or locally.
# ------------------------------------------------------------------
build-docker: frontend-docker build-go-docker
build: frontend build-go-local


# ------------------------------------------------------------------
# 4️⃣ Build the frontend locally.
# ------------------------------------------------------------------
frontend:
	@echo "Building frontend locally…"
	@corepack enable && corepack prepare pnpm@10.23.0 --activate && pnpm --dir frontend install --frozen-lockfile && pnpm --dir frontend build


# ------------------------------------------------------------------
# 5️⃣ Build the Go binaries for all supported architectures (cross‑compile) locally.
# ------------------------------------------------------------------
build-go-local: frontend
	@echo "Building Go binaries for all supported architectures…"
	@for os in $(OSES); do \
	    for arch in $(ARCHES); do \
	        mkdir -p bin/$$os-$$arch && \
	        GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.BuildID=$(VERSION)" -o bin/$$os-$$arch/logthing ./cmd/server && \
	        go build -trimpath -o bin/$$os-$$arch/syslogsend ./cmd/syslogsend; \
	    done; \
	done

# ------------------------------------------------------------------
# 5️⃣ Existing test target – still depends on local frontend files.
# ------------------------------------------------------------------
test: frontend-docker
	go test ./...

syslogsend:
	go run ./cmd/syslogsend -network udp -addr 127.0.0.1:5514 -message "logthing Makefile test event"

docker-build:
	$(DOCKER) build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(TAG) .

docker-run:
	$(DOCKER) run --rm \
		-p 8080:8080 \
		-p 5514:5514/tcp \
		-p 5514:5514/udp \
		-v "$(DATA_DIR):/data" \
		-e LOGTHING_USERNAME=admin \
		-e LOGTHING_PASSWORD=secret \
		$(IMAGE):$(TAG)

docker-login:
	$(DOCKER) login

docker-push:
	$(DOCKER) push $(IMAGE):$(TAG)

comma := ,
empty :=
space := $(empty) $(empty)

PLATFORMS := $(foreach os,$(OSES),$(foreach arch,$(ARCHES),$(os)/$(arch)))
PLATFORMS := $(subst $(space),$(comma),$(PLATFORMS))
docker-buildx-push:
	$(DOCKER_BUILDX) build \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(TAG) \
		--push .

compose-up:
	$(DOCKER) compose up --build

compose-down:
	$(DOCKER) compose down

check-release-clean:
	@git diff --quiet || (echo "Worktree has unstaged changes"; exit 1)
	@git diff --cached --quiet || (echo "Worktree has staged changes"; exit 1)

check-release-main:
	@test "$$(git branch --show-current)" = "main" || (echo "Not on main"; exit 1)

release-tag: check-release-clean check-release-main
	@test -n "$(TAG)" || (echo "Usage: make release-tag TAG=v0.1.2"; exit 1)
	git fetch --tags origin
	git pull --ff-only origin main
	git tag -a $(TAG) -m "$(TAG)"
	git push origin $(TAG)

release-patch: check-release-clean check-release-main
	@git fetch --tags origin
	@git pull --ff-only origin main
	@latest=$$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1); \
	if [ -z "$$latest" ]; then \
		next=v0.1.0; \
	else \
		version=$${latest#v}; \
		major=$${version%%.*}; \
		rest=$${version#*.}; \
		minor=$${rest%%.*}; \
		patch=$${rest#*.}; \
		next=v$$major.$$minor.$$((patch + 1)); \
	fi; \
	echo "Tagging $$next"; \
	git tag -a $$next -m "$$next"; \
	git push origin $$next
