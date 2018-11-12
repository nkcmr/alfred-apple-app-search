GO_SOURCES = $(shell ls *.go)

broken_image.go: broken_image.png

alfred-apple-app-search: $(GO_SOURCES) broken_image.go
	GOOS=darwin go build -v \
		-ldflags='-w -s' \
		.
