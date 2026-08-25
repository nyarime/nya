module github.com/nyarime/nya

go 1.25.0

require (
	github.com/nyarime/gofec/v2 v2.0.0
	golang.org/x/sys v0.47.0
)

require golang.org/x/crypto v0.55.0

require github.com/nyarime/compress v0.1.0

replace github.com/nyarime/compress => ../compress
