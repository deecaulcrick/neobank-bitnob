// Command whoami is the M0 health check: it proves the HMAC signing and
// credentials work by calling Bitnob's GET /api/whoami.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
)

func main() {
	id, secret := os.Getenv("BITNOB_CLIENT_ID"), os.Getenv("BITNOB_CLIENT_SECRET")
	if id == "" || secret == "" {
		fmt.Fprintln(os.Stderr, "set BITNOB_CLIENT_ID and BITNOB_CLIENT_SECRET")
		os.Exit(1)
	}
	base := os.Getenv("BITNOB_BASE_URL")
	if base == "" {
		base = "https://api.bitnob.com"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := bitnob.New(base, id, secret).WhoAmI(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}
