module github.com/jotadrilo/webp/bench

go 1.26.4

require (
	github.com/eringen/gowebper v0.1.2
	github.com/gen2brain/vpx v0.2.2
	github.com/gen2brain/webp v0.6.4
	github.com/jotadrilo/webp v0.0.0
	golang.org/x/image v0.47.0
)

require github.com/ebitengine/purego v0.10.1 // indirect

replace github.com/jotadrilo/webp => ../
