// Command signrelease signs a release's SHA256SUMS with the ed25519 key
// whose public half internal/update trusts, writing <file>.sig.
//
//	signrelease -keygen <file>  # writes a new private key to <file>, prints the public key
//	signrelease SHA256SUMS      # signs with the key in $RELEASE_SIGNING_KEY
//
// Keys are base64: the private key as its 32-byte seed, the public key as
// its 32 bytes (internal/update's publicKey). Nothing secret is printed.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	keygen := flag.String("keygen", "", "write a new private key to this file and print its public key")
	flag.Parse()
	log.SetFlags(0)
	if *keygen != "" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal(err)
		}
		seed := base64.StdEncoding.EncodeToString(priv.Seed())
		f, err := os.OpenFile(*keygen, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := f.WriteString(seed + "\n"); err != nil {
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		return
	}
	if flag.NArg() != 1 {
		log.Fatal("usage: signrelease SHA256SUMS (key in $RELEASE_SIGNING_KEY), or signrelease -keygen <file>")
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("RELEASE_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		log.Fatal("RELEASE_SIGNING_KEY isn't a base64 ed25519 seed")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	file := flag.Arg(0)
	data, err := os.ReadFile(file)
	if err != nil {
		log.Fatal(err)
	}
	sig := ed25519.Sign(priv, data)
	if err := os.WriteFile(file+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("signed %s with public key %s\n", file, base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)))
}
