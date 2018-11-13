GO_SOURCES = $(shell ls *.go)

alfred-apple-app-search: $(GO_SOURCES)
	GOOS=darwin go build -v \
		-ldflags='-w -s' \
		.
