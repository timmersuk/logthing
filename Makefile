IMAGE ?= timmersuk/logthing
TAG ?= latest
DATA_DIR ?= $(CURDIR)/logthing-data
PLATFORMS ?= linux/amd64,linux/arm64
DOCKER ?= docker
DOCKER_BUILDX ?= docker buildx
VERSION ?= $(TAG)

.PHONY: frontend test build syslogsend docker-build docker-run docker-login docker-push docker-buildx-push compose-up compose-down check-release-clean check-release-main release-tag release-patch

frontend:
	pnpm --dir frontend install --frozen-lockfile
	pnpm --dir frontend build

test: frontend
	go test ./...

build: frontend
	go build -trimpath -ldflags "-X main.BuildID=$(VERSION)" -o bin/logthing ./cmd/server
	go build -trimpath -o bin/syslogsend ./cmd/syslogsend

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
