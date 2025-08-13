BINARY_NAME=myapp
SRC=.

build-linux:
	mkdir -p build
	GOOS=linux GOARCH=amd64 go build -o build/$(BINARY_NAME)-linux-amd64 $(SRC)

build-windows:
	mkdir -p build
	GOOS=windows GOARCH=amd64 go build -o build/$(BINARY_NAME)-windows-amd64.exe $(SRC)
