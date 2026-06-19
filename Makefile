IMAGE ?= timmersuk/logthing
TAG ?= latest
DATA_DIR ?= $(CURDIR)/logthing-data
PLATFORMS ?= linux/amd64,linux/arm64
DOCKER ?= docker
DOCKER_BUILDX ?= docker buildx
VERSION ?= $(TAG)

.PHONY: frontend test build syslogsend docker-build docker-run docker-login docker-push docker-buildx-push compose-up compose-down

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
