module github.com/goforj/secret/examples

go 1.25.12

require (
	github.com/goforj/secret v0.1.0
	github.com/goforj/secret/driver/filesecret v0.1.0
	github.com/goforj/secret/secrettest v0.1.0
)

replace github.com/goforj/secret => ..

replace github.com/goforj/secret/driver/filesecret => ../driver/filesecret

replace github.com/goforj/secret/secrettest => ../secrettest
