.PHONY: build clean

BIN_DIR := bin
BINS := sms-gateway sms-gateway-watch

build: $(addprefix $(BIN_DIR)/,$(BINS))

$(BIN_DIR)/%: cmd/%/main.go
	mkdir -p $(BIN_DIR)
	go build -o $@ ./cmd/$*

clean:
	rm -rf $(BIN_DIR)
