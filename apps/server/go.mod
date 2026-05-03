module github.com/jumoel/hackathon/apps/server

go 1.23

require (
	github.com/coder/websocket v1.8.14
	github.com/jumoel/hackathon/packages/go-shared v0.0.0-00010101000000-000000000000
)

replace github.com/jumoel/hackathon/packages/go-shared => ../../packages/go-shared
