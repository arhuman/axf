module github.com/arhuman/axf

go 1.25.7

// Build with a patched toolchain (>= the floor) so locally-built binaries pick
// up stdlib security fixes; the lower `go` line keeps module consumers on 1.25+.
toolchain go1.26.5

require github.com/santhosh-tekuri/jsonschema/v5 v5.3.1
