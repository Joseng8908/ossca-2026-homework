BINARY := app

.PHONY: build run clean

build:
	go build -o $(BINARY) .

run: build
	sudo ./$(BINARY)

clean:
	rm -f $(BINARY)
