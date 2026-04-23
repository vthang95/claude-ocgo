BIN := ocgo

.PHONY: build clean

build:
	go build -o $(BIN) .

clean:
	rm -f $(BIN)
