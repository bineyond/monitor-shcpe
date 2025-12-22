.PHONY: build clean run

BINARY_NAME=monitor

build:
	go build -o $(BINARY_NAME) main.go

clean:
	rm -f $(BINARY_NAME)
	rm -f debug_page.html
	rm -f monitor.log

run: build
	./run_monitor.sh
