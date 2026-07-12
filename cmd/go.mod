module github.com/mtrisic/vremeplov/cmd

go 1.24

require (
	github.com/mtrisic/vremeplov/core v0.0.0-00010101000000-000000000000
	github.com/mtrisic/vremeplov/roms v0.0.0-00010101000000-000000000000
)

require (
	github.com/google/go-dap v0.12.0 // indirect
	github.com/mtrisic/gozilog v1.1.1 // indirect
)

// Intra-repo modules are unpublished; resolve them relatively (works in
// both module and workspace mode, no network).
replace (
	github.com/mtrisic/vremeplov/core => ../core
	github.com/mtrisic/vremeplov/roms => ../roms
)
