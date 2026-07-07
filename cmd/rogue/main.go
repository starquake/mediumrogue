// Command rogue is the medium-rogue game server: it runs the authoritative
// world simulation and serves the embedded browser client.
package main

import (
	"context"
	"os"

	"github.com/starquake/medium-rogue/cmd/rogue/app"
)

func main() {
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stderr))
}
