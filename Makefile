.PHONY: build clean test test-integration vet scan scan-history hooks deploy_pi deploy-tunnel deploy-watch install-tunnel-service install-watch-service install-watch-pi

BIN_DIR := bin
BINS := sms-gateway sms-gateway-watch sms-gateway-scan
GO_SRC := $(shell find cmd internal -name '*.go') go.mod go.sum

PI_HOST ?= first_pi
PI_DIR := ~/smsgateway
PI_GO_PATH := export PATH=$$HOME/.local/go1.26/bin:$$PATH

build: $(addprefix $(BIN_DIR)/,$(BINS))

$(BIN_DIR)/%: $(GO_SRC)
	mkdir -p $(BIN_DIR)
	go build -o $@ ./cmd/$*

clean:
	rm -rf $(BIN_DIR)

test:
	go test ./... -race -count=1

# The serial lab runs the real driver against a pseudo-terminal whose device
# node is removed mid-test, which is the USB re-enumeration the unit tests
# cannot reproduce. Needs /dev/ptmx and the integration build tag, so it is
# deliberately not part of `make test`.
test-integration:
	go test -tags=integration -count=1 -timeout 5m ./test/integration/...

# This repository is public and its fixtures come from real SMS, so a SIM
# serial, a subscriber number, a card tail or a balance can reach a commit just
# by being the easiest way to write a meaningful test. `scan` checks the working
# tree; `scan-history` checks every blob in every ref, which is the one that
# tells you whether an old commit is still carrying something.
scan: build
	$(BIN_DIR)/sms-gateway-scan

scan-history: build
	$(BIN_DIR)/sms-gateway-scan -history

# Installs the scanner as this clone's pre-commit and pre-push hooks. Git does
# not clone hooks, so this is per-checkout and has to be run again after a
# fresh clone.
hooks: build
	$(BIN_DIR)/sms-gateway-scan -install-hooks

# Both invocations: the default one does not vet the integration-tagged files,
# which is where the lab lives.
vet:
	go vet ./...
	go vet -tags=integration ./test/...

deploy_pi:
	rsync -avz --exclude='bin/' --exclude='config.yaml' --exclude='data/' ./ $(PI_HOST):$(PI_DIR)

deploy-tunnel: deploy_pi
	ssh -t $(PI_HOST) 'sudo cp $(PI_DIR)/deploy/systemd/sms-tunnel.service /etc/systemd/system/sms-tunnel.service && sudo systemctl daemon-reload && sudo systemctl enable sms-tunnel && sudo systemctl restart sms-tunnel'

deploy-watch: deploy_pi
	ssh -t $(PI_HOST) '$(PI_GO_PATH); cd $(PI_DIR) && make install-watch-pi'

install-watch-pi: build
	sudo cp deploy/systemd/sms-gateway-watch.pi.service /etc/systemd/system/sms-gateway-watch.service
	sudo systemctl daemon-reload
	sudo systemctl enable sms-gateway-watch
	sudo systemctl restart sms-gateway-watch

install-watch-service: build
	sudo cp bin/sms-gateway-watch /usr/local/bin/
	sudo cp deploy/systemd/sms-gateway-watch.service /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl enable sms-gateway-watch
	sudo systemctl restart sms-gateway-watch

install-tunnel-service:
	sudo cp deploy/systemd/sms-tunnel.service /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl enable sms-tunnel
	sudo systemctl restart sms-tunnel

mount-pi-db:
	@mkdir -p /home/legion/github/sms-gateway/data/pi-db
	@sshfs first_pi:/home/legion/smsgateway/data /home/legion/github/sms-gateway/data/pi-db
	@ln -sf /home/legion/github/sms-gateway/data/pi-db/sms.db /home/legion/github/sms-gateway/data/pi-sms.db
unmount-pi-db:
	@rm -f /home/legion/github/sms-gateway/data/pi-sms.db
	@fusermount -u /home/legion/github/sms-gateway/data/pi-db