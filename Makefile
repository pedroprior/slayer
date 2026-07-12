SHELL := /usr/bin/env bash
BIN         := clusterctl
CONFIG      := cluster.yaml
TALOS_DIR   := talos
KUBECONFIG_FILE := $(TALOS_DIR)/kubeconfig
TALOS_ISO_URL  := https://github.com/siderolabs/talos/releases/latest/download/metal-amd64.iso
TALOS_ISO_PATH := /var/lib/libvirt/images/metal-amd64.iso

.PHONY: help build install test test-shell vet fmt tidy clean \
        download-talos-iso provision bootstrap addons status stop destroy \
        kubeconfig nodes cluster-info

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the clusterctl binary into ./bin
	go build -o bin/$(BIN) ./cmd/clusterctl

install: ## Install clusterctl to $GOPATH/bin
	go install ./cmd/clusterctl

test: ## Run unit tests
	go test ./...

test-shell: ## Run shell-level tests for Makefile targets (e.g. download-talos-iso)
	./test/download-talos-iso.sh

vet: ## Run go vet
	go vet ./...

fmt: ## Format source
	go fmt ./...

tidy: ## Tidy go.mod/go.sum
	go mod tidy

clean: ## Remove build artifacts
	rm -rf bin

## --- Cluster lifecycle (wraps clusterctl; requires libvirt/QEMU + talosctl) ---

download-talos-iso: ## Download Talos metal-amd64.iso into libvirt images dir (skips if already present)
	@if [ -f $(TALOS_ISO_PATH) ]; then \
		echo "ISO already present at $(TALOS_ISO_PATH), skipping download"; \
	else \
		echo "Downloading Talos ISO to $(TALOS_ISO_PATH)..."; \
		sudo wget -O $(TALOS_ISO_PATH) $(TALOS_ISO_URL); \
	fi

provision: build download-talos-iso ## Create/start the control-plane and worker VMs
	./bin/$(BIN) --config $(CONFIG) provision

bootstrap: build ## Generate/apply Talos configs, bootstrap etcd, fetch kubeconfig
	./bin/$(BIN) --config $(CONFIG) bootstrap

addons: build ## Apply Kubernetes addon manifests (MetalLB)
	./bin/$(BIN) --config $(CONFIG) addons

status: build ## Show libvirt-level status of the cluster's VMs
	./bin/$(BIN) --config $(CONFIG) status

stop: build ## Gracefully shut down all cluster VMs, keeping them defined
	./bin/$(BIN) --config $(CONFIG) stop

destroy: build ## Stop and undefine all cluster VMs (DESTRUCTIVE, requires confirmation)
	./bin/$(BIN) --config $(CONFIG) destroy --yes

## --- Convenience kubectl/talosctl helpers ---

kubeconfig: ## Print the export command for KUBECONFIG
	@echo "export KUBECONFIG=$(CURDIR)/$(KUBECONFIG_FILE)"

nodes: ## List cluster nodes (requires KUBECONFIG exported)
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl get nodes -o wide

cluster-info: ## Show cluster-info (requires KUBECONFIG exported)
	KUBECONFIG=$(KUBECONFIG_FILE) kubectl cluster-info
