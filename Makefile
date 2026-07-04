.PHONY: build clean deploy_pi deploy-tunnel deploy-watch install-tunnel-service install-watch-service install-watch-pi

BIN_DIR := bin
BINS := sms-gateway sms-gateway-watch

PI_HOST ?= first_pi
PI_DIR := ~/smsgateway
PI_GO_PATH := export PATH=$$HOME/.local/go1.26/bin:$$PATH

build: $(addprefix $(BIN_DIR)/,$(BINS))

$(BIN_DIR)/%: cmd/%/main.go
	mkdir -p $(BIN_DIR)
	go build -o $@ ./cmd/$*

clean:
	rm -rf $(BIN_DIR)

deploy_pi:
	rsync -avz --exclude='bin/' --exclude='config.yaml' ./ $(PI_HOST):$(PI_DIR)

deploy-tunnel: deploy_pi
	ssh $(PI_HOST) 'sudo cp $(PI_DIR)/deploy/systemd/sms-tunnel.service /etc/systemd/system/sms-tunnel.service && sudo systemctl daemon-reload && sudo systemctl enable sms-tunnel && sudo systemctl restart sms-tunnel'

deploy-watch: deploy_pi
	ssh $(PI_HOST) '$(PI_GO_PATH); cd $(PI_DIR) && make install-watch-pi'

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