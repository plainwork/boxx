.PHONY: build run doctor tidy clean release

build:
	go build -o boxx .

run: build
	./boxx

doctor: build
	./boxx doctor

tidy:
	go mod tidy

clean:
	rm -rf boxx dist

# Usage: make release VERSION=v0.1.0
release:
	@[ -n "$(VERSION)" ] || (echo "usage: make release VERSION=v0.1.0" && exit 1)
	git tag -a $(VERSION) -m "release $(VERSION)"
	git push origin $(VERSION)
