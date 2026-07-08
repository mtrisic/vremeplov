module github.com/mtrisic/vremeplov/frontends/wasm

go 1.24.0

require (
	github.com/mtrisic/vremeplov/core v0.0.0-00010101000000-000000000000
	github.com/mtrisic/vremeplov/roms v0.0.0-00010101000000-000000000000
)

require github.com/mtrisic/gozilog v1.1.1 // indirect

// Intra-repo modules are unpublished; resolve them relatively.
replace (
	github.com/mtrisic/vremeplov/core => ../../core
	github.com/mtrisic/vremeplov/roms => ../../roms
)
