package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
        "log"

	"github.com/kriipke/copilot-extension/agent"
	"github.com/kriipke/copilot-extension/config"
	"github.com/kriipke/copilot-extension/oauth"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	host := os.Getenv("HOST")
	port := os.Getenv("PORT")
	if host == "" || port == "" {
		return fmt.Errorf("HOST and PORT environment variables must be set")
	}

	pubKey, err := fetchPublicKey()
	if err != nil {
			log.Fatalf("failed to fetch public key: %v", err)
	}

	config, err := config.New()
	if err != nil {
		return fmt.Errorf("error fetching config: %w", err)
	}
	me, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("unable to parse HOST environment variable: %w", err)
	}

	me.Path = "auth/callback"

	oauthService := oauth.NewService(config.ClientID, config.ClientSecret, me.String())
	http.HandleFunc("/auth/authorization", oauthService.PreAuth)
	http.HandleFunc("/auth/callback", oauthService.PostAuth)

	agentService := agent.NewService(pubKey)

	http.HandleFunc("/agent", agentService.ChatCompletion)

	fmt.Println("Listening on port", port)
	return http.ListenAndServe(":"+port, nil)
}

// fetchPublicKey fetches the keys used to sign messages from copilot.  Checking
// the signature with one of these keys verifies that the request to the
// completions API comes from GitHub and not elsewhere on the internet.
func fetchPublicKey() (*ecdsa.PublicKey, error) {
	resp, err := http.Get("https://api.github.com/meta/public_keys/copilot_api")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch public key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch public key: %s", resp.Status)
	}

	var respBody struct {
		PublicKeys []struct {
			Key       string `json:"key"`
			IsCurrent bool   `json:"is_current"`
		} `json:"public_keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}

	var rawKey string
	for _, pk := range respBody.PublicKeys {
		if pk.IsCurrent {
			rawKey = pk.Key
			break
		}
	}
	if rawKey == "" {
		return nil, fmt.Errorf("could not find current public key")
	}

	pubPemStr := strings.ReplaceAll(rawKey, "\\n", "\n")
	// Decode the Public Key
	block, _ := pem.Decode([]byte(pubPemStr))
	if block == nil {
		return nil, fmt.Errorf("error parsing PEM block with GitHub public key")
	}

	// Create our ECDSA Public Key
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	// Because of documentation, we know it's a *ecdsa.PublicKey
	ecdsaKey, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("GitHub key is not ECDSA")
	}

	return ecdsaKey, nil
}
