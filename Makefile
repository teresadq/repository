.PHONY: web build test run clean

web:
	cd web && npm install --no-audit --no-fund && npm run build

build: web
	mkdir -p bin
	go build -trimpath -buildvcs=false -o bin/kkrepo-go ./cmd/server

test:
	go test ./...

run: build
	./bin/kkrepo-go

clean:
	rm -rf bin web/dist/assets web/dist/index.html web/dist/favicon.svg web/dist/static
