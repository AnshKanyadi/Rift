module github.com/anshkanyadi/rift

// Language version floor. The exact toolchain is pinned below; CI reads this
// file (setup-go: go-version-file) so it runs precisely this version and cannot
// drift from local development.
go 1.22.0

toolchain go1.22.5

require golang.org/x/tools v0.30.0

require (
	golang.org/x/mod v0.23.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
)
